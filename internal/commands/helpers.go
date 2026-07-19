package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/config"
)

// newClient loads the config named by the root --config flag and returns an
// authenticated API client. Every command action starts with this one call.
func newClient(cmd *cli.Command) (*client.Client, error) {
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return nil, err
	}
	return client.New(cfg)
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
