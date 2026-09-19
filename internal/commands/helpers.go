package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/config"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// newClient loads the config named by the root --config flag and returns an
// authenticated API client. Every command action starts with this one call.
// It prints the current user identity to stderr for safety.
func newClient(ctx context.Context, cmd *cli.Command) (*client.Client, error) {
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return nil, err
	}
	c, err := client.New(cfg)
	if err != nil {
		return nil, err
	}
	c.PrintIdentity(ctx)
	// Warn-only gate: an old server still runs (legacy fallbacks apply
	// where they exist), but the user always sees the mismatch.
	if err := c.CheckServerVersion(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ %v\n", err)
	}
	return c, nil
}

// resolveAlbumByName resolves exactly one album by exact name. Phone
// keyboards often leave trailing spaces in album names, so when the exact
// lookup fails it retries whitespace-tolerantly: if exactly one album's
// name trims to the query, offerWhitespaceAlbum asks whether to use it
// (auto-accepted when autoYes, e.g. behind --yes). Any other outcome
// returns the original lookup error, so scripts with exact names and
// closed stdin behave exactly as before.
func resolveAlbumByName(ctx context.Context, c *client.Client, stdin io.Reader, stdout io.Writer, query string, autoYes bool) (immichapi.AlbumResponseDto, error) {
	album, err := workflows.ResolveAlbum(ctx, c, nil, query)
	if err == nil {
		return album, nil
	}
	candidates, ferr := searchAlbumsByTrimmedName(ctx, c, query)
	if ferr != nil || len(candidates) != 1 {
		return immichapi.AlbumResponseDto{}, err
	}
	return offerWhitespaceAlbum(stdin, stdout, query, candidates[0], autoYes, err)
}

// searchAlbumsByTrimmedName lists all albums (GET /albums) and returns those
// whose name trims to the query (both sides trimmed with TrimSpace).
func searchAlbumsByTrimmedName(ctx context.Context, c *client.Client, query string) ([]immichapi.AlbumResponseDto, error) {
	resp, err := c.API.GetAllAlbumsWithResponse(ctx, &immichapi.GetAllAlbumsParams{})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return nil, fmt.Errorf("listing albums: %w", err)
	}
	var out []immichapi.AlbumResponseDto
	if resp.JSON200 != nil {
		out = matchTrimmedAlbumName(*resp.JSON200, query)
	}
	return out, nil
}

// matchTrimmedAlbumName returns the albums whose name trims to the query
// (both sides trimmed). Pure (no I/O) so the matching is directly
// unit-testable.
func matchTrimmedAlbumName(albums []immichapi.AlbumResponseDto, query string) []immichapi.AlbumResponseDto {
	want := strings.TrimSpace(query)
	var out []immichapi.AlbumResponseDto
	for _, a := range albums {
		if strings.TrimSpace(a.AlbumName) == want {
			out = append(out, a)
		}
	}
	return out
}

// offerWhitespaceAlbum asks whether to use the single whitespace-variant
// candidate found for query (auto-accepted when autoYes); declining — or a
// closed stdin — returns origErr, the exact-lookup failure. Pure except for
// the prompt I/O, which tests drive with buffers.
func offerWhitespaceAlbum(stdin io.Reader, stdout io.Writer, query string, candidate immichapi.AlbumResponseDto, autoYes bool, origErr error) (immichapi.AlbumResponseDto, error) {
	if autoYes {
		fmt.Fprintf(stdout, "No album named %q found; using %q (--yes).\n", query, candidate.AlbumName)
		return candidate, nil
	}
	fmt.Fprintf(stdout, "No album named %q found, but found %q (%s). Use it? [y/N]: ",
		query, candidate.AlbumName, whitespaceNote(candidate.AlbumName))
	if !confirm(stdin) {
		return immichapi.AlbumResponseDto{}, origErr
	}
	return candidate, nil
}

// whitespaceNote describes which end of name carries whitespace, for the
// offer prompt (e.g. "trailing whitespace").
func whitespaceNote(name string) string {
	var bits []string
	if strings.TrimLeft(name, " \t\r\n\v\f") != name {
		bits = append(bits, "leading whitespace")
	}
	if strings.TrimRight(name, " \t\r\n\v\f") != name {
		bits = append(bits, "trailing whitespace")
	}
	if len(bits) == 0 {
		return "whitespace"
	}
	return strings.Join(bits, " and ")
}

// idsFileFlag is the shared flag for bulk commands that read asset IDs from a
// file (one UUID per line; "-" means stdin).
func idsFileFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "ids-file",
		Usage: "read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)",
	}
}

// collectIDs gathers asset IDs from positional arguments and --ids-file.
// At least one ID must be provided from either source.
func collectIDs(cmd *cli.Command) ([]openapi_types.UUID, error) {
	raw := cmd.Args().Slice()

	if path := cmd.String("ids-file"); path != "" {
		fileIDs, err := readIDLines(path)
		if err != nil {
			return nil, err
		}
		raw = append(raw, fileIDs...)
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("no asset IDs given: pass them as arguments or via --ids-file")
	}

	ids := make([]openapi_types.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid asset ID %q: %w", s, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// readIDLines reads one ID per line from path ("-" = stdin). Blank lines and
// comment lines (starting with '#' or '//') are skipped.
func readIDLines(path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("opening ids file: %w", err)
		}
		defer f.Close()
		r = f
	}

	var ids []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if isBlankOrComment(line) {
			continue
		}
		ids = append(ids, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading ids from %q: %w", path, err)
	}
	return ids, nil
}

// isBlankOrComment reports whether a trimmed line from an input file should
// be skipped: empty, or a comment starting with '#' or '//'. Shared by every
// line-oriented file reader (--ids-file, --replace-file, ...) so comment
// syntax stays consistent across the CLI.
func isBlankOrComment(line string) bool {
	return line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//")
}

// formatBytes renders an optional byte count human-readably ("unlimited" when nil).
func formatBytes(n *int) string {
	if n == nil {
		return "unlimited"
	}
	const unit = 1024
	if *n < unit {
		return fmt.Sprintf("%d B", *n)
	}
	div, exp := unit, 0
	for v := *n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(*n)/float64(div), "KMGTPE"[exp])
}
