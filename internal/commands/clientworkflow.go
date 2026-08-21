package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/config"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// ClientWorkflow returns the `client-workflow` command group (alias `cw`):
// multi-step orchestrations that run entirely client-side, combining several
// Immich API calls with local processing. Not to be confused with Immich's
// own server-side "Workflows" API tag.
func ClientWorkflow() *cli.Command {
	return &cli.Command{
		Name:    "client-workflow",
		Aliases: []string{"cw"},
		Usage:   "Client-side multi-step workflows",
		CommandNotFound: func(_ context.Context, cmd *cli.Command, name string) {
			fmt.Fprintf(os.Stderr, "Unknown workflow %q. Run '%s --help' to see available workflows.\n", name, cmd.FullName())
		},
		Commands: []*cli.Command{
			replaceAssetCommand(),
			tagDeleteCommand(),
			addUsersToAlbumWithPatternCommand(),
			findNoThumbhashCommand(),
			findHEICTileDefectCommand(),
			repairAssetsCommand(),
			fixAlbumDatesCommand(),
			downloadAlbumCommand(),
		},
	}
}

func replaceAssetCommand() *cli.Command {
	return &cli.Command{
		Name:      "replace-asset",
		Usage:     "Replace an existing asset with a new file, keeping its metadata",
		ArgsUsage: "[ASSET_ID NEW_FILE_PATH]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "replace-file",
				Usage: "read asset-id;new-file-path pairs from `FILE`, one per line ('-' for stdin; '#' or '//' starts a comment)",
			},
			&cli.BoolFlag{
				Name:  "dont-remove-original-file",
				Usage: "keep the original asset instead of removing it after a successful replacement",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "permanently delete the original asset instead of moving it to trash",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "print the planned steps for each pair without changing anything",
			},
			&cli.BoolFlag{
				Name:  "yes",
				Usage: "skip the confirmation prompt before removing original assets",
			},
		},
		Action: clientWorkflowReplaceAsset,
	}
}

func tagDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "tag-delete",
		Usage: "Delete tags whose full path matches an include/exclude regex",
		Description: "Fetches all tags, keeps those whose full path (Value) matches --include " +
			"and not --exclude, shows every tag that would be deleted, then deletes them. " +
			"Deletion is PERMANENT (the Tags API has no trash). Deleting a parent tag also " +
			"deletes its child tags server-side.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "include",
				Usage: "only delete tags whose full path matches this `REGEX` (default: match all)",
			},
			&cli.StringFlag{
				Name:  "exclude",
				Usage: "never delete tags whose full path matches this `REGEX`",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "print the tags that would be deleted without changing anything",
			},
			&cli.BoolFlag{
				Name:  "yes",
				Usage: "skip the confirmation prompt before deleting tags",
			},
		},
		Action: clientWorkflowTagDelete,
	}
}

func clientWorkflowTagDelete(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() > 0 {
		return fmt.Errorf("tag-delete takes no positional arguments; pass filters as --include/--exclude REGEX flags (got %v)", cmd.Args().Slice())
	}

	include, err := compileOptionalRegex(cmd.String("include"))
	if err != nil {
		return fmt.Errorf("invalid --include: %w", err)
	}
	exclude, err := compileOptionalRegex(cmd.String("exclude"))
	if err != nil {
		return fmt.Errorf("invalid --exclude: %w", err)
	}

	dryRun := cmd.Bool("dry-run")
	yes := cmd.Bool("yes")

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	opts := workflows.TagDeleteOptions{
		Include: include,
		Exclude: exclude,
		DryRun:  dryRun,
	}

	tags, err := workflows.SelectTagsForDeletion(ctx, c, opts)
	if err != nil {
		return err
	}

	if len(tags) == 0 {
		fmt.Println("No tags matched; nothing to delete.")
		return nil
	}

	fmt.Printf("%d tag(s) would be deleted:\n", len(tags))
	for _, t := range tags {
		fmt.Printf("  %s  %s\n", t.Id, t.Value)
	}
	fmt.Println("Warning: deletion is PERMANENT (tags have no trash) and deleting a parent tag also deletes its children.")

	if !dryRun && !yes {
		fmt.Printf("Permanently delete these %d tag(s)? [y/N]: ", len(tags))
		if !confirm(os.Stdin) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	return workflows.DeleteTags(ctx, c, tags, opts)
}

// compileOptionalRegex compiles pattern, returning (nil, nil) for an empty
// pattern (meaning "no filter"). A non-empty invalid pattern returns an error.
func compileOptionalRegex(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

func addUsersToAlbumWithPatternCommand() *cli.Command {
	return &cli.Command{
		Name:  "add-users-to-album-with-pattern",
		Usage: "Share every album whose name matches an include/exclude regex with a user",
		Description: "Resolves --user to exactly one person (exact UUID, or a case-insensitive " +
			"substring match on name/email), fetches all albums, keeps those whose name matches " +
			"--include and not --exclude, shows the full list, then shares each of them with that " +
			"user at --role. Albums where the user already has access are skipped.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "include",
				Usage: "only share albums whose name matches this `REGEX` (default: match all)",
			},
			&cli.StringFlag{
				Name:  "exclude",
				Usage: "never share albums whose name matches this `REGEX`",
			},
			&cli.StringFlag{
				Name:     "user",
				Usage:    "target user: exact user `UUID`, or a case-insensitive substring of their name/email",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "role",
				Usage: "album role to grant: editor, viewer, or owner",
				Value: string(immichapi.AlbumUserRoleViewer),
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "print the albums that would be shared without changing anything",
			},
			&cli.BoolFlag{
				Name:    "interactive",
				Aliases: []string{"i"},
				Usage:   "ask once per album (y/N) instead of one bulk confirmation; --yes overrides this and skips all prompts",
			},
			&cli.BoolFlag{
				Name:  "yes",
				Usage: "skip the confirmation prompt(s) before sharing albums",
			},
		},
		Action: clientWorkflowAddUsersToAlbumWithPattern,
	}
}

func clientWorkflowAddUsersToAlbumWithPattern(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() > 0 {
		return fmt.Errorf("add-users-to-album-with-pattern takes no positional arguments; pass filters as --include/--exclude REGEX flags (got %v)", cmd.Args().Slice())
	}

	include, err := compileOptionalRegex(cmd.String("include"))
	if err != nil {
		return fmt.Errorf("invalid --include: %w", err)
	}
	exclude, err := compileOptionalRegex(cmd.String("exclude"))
	if err != nil {
		return fmt.Errorf("invalid --exclude: %w", err)
	}

	role := immichapi.AlbumUserRole(cmd.String("role"))
	switch role {
	case immichapi.AlbumUserRoleEditor, immichapi.AlbumUserRoleViewer, immichapi.AlbumUserRoleOwner:
		// valid
	default:
		return fmt.Errorf("invalid --role %q: must be editor, viewer, or owner", role)
	}

	dryRun := cmd.Bool("dry-run")
	interactive := cmd.Bool("interactive")
	yes := cmd.Bool("yes")

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Resolve the target user first: fail fast on an ambiguous/unknown
	// query before touching any album.
	user, err := workflows.ResolveUser(ctx, c, cmd.String("user"))
	if err != nil {
		return err
	}
	fmt.Printf("Target user: %s <%s> (%s)\n", user.Name, user.Email, user.Id)

	opts := workflows.AddUsersToAlbumOptions{
		Include: include,
		Exclude: exclude,
		Role:    role,
		DryRun:  dryRun,
	}

	albums, err := workflows.SelectAlbumsForSharing(ctx, c, opts)
	if err != nil {
		return err
	}

	if len(albums) == 0 {
		fmt.Println("No albums matched; nothing to share.")
		return nil
	}

	fmt.Printf("%d album(s) would be shared with %s as %s:\n", len(albums), user.Name, role)
	for _, a := range albums {
		note := ""
		if _, already := workflows.AlbumHasUser(a, user.Id); already {
			note = "  (already shared)"
		}
		fmt.Printf("  %s  %s  %d asset(s)%s\n", a.Id, a.AlbumName, a.AssetCount, note)
	}

	// --interactive asks once per album instead of one bulk confirmation;
	// --yes always wins and skips every prompt (bulk or per-album).
	if interactive && !yes {
		albums = selectAlbumsInteractively(os.Stdin, os.Stdout, albums, *user, role)
		if len(albums) == 0 {
			fmt.Println("No albums selected; nothing to share.")
			return nil
		}
	} else if !dryRun && !yes {
		fmt.Printf("Share these %d album(s) with %s? [y/N]: ", len(albums), user.Name)
		if !confirm(os.Stdin) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	return workflows.ShareAlbumsWithUser(ctx, c, albums, *user, opts)
}

// selectAlbumsInteractively asks once per album (via in/out) whether to
// share it with user, and returns the subset the caller confirmed. Albums
// where user already has access are kept without asking — they are already
// no-ops for ShareAlbumsWithUser and skipped there with an info message.
func selectAlbumsInteractively(in io.Reader, out io.Writer, albums []immichapi.AlbumResponseDto, user immichapi.UserResponseDto, role immichapi.AlbumUserRole) []immichapi.AlbumResponseDto {
	scanner := bufio.NewScanner(in)
	var selected []immichapi.AlbumResponseDto
	for _, a := range albums {
		if _, already := workflows.AlbumHasUser(a, user.Id); already {
			selected = append(selected, a)
			continue
		}

		fmt.Fprintf(out, "Share %q (%d asset(s)) with %s as %s? [y/N]: ", a.AlbumName, a.AssetCount, user.Name, role)
		if !scanner.Scan() {
			break
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer == "y" || answer == "yes" {
			selected = append(selected, a)
		}
	}
	return selected
}

func clientWorkflowReplaceAsset(ctx context.Context, cmd *cli.Command) error {
	pairs, err := collectReplacePairs(cmd)
	if err != nil {
		return err
	}

	dryRun := cmd.Bool("dry-run")
	keepOriginal := cmd.Bool("dont-remove-original-file")
	force := cmd.Bool("force")
	yes := cmd.Bool("yes")

	// The destructive removal step requires confirmation unless it isn't
	// going to run at all (--dont-remove-original-file) or this is a dry
	// run (nothing is actually deleted).
	if !keepOriginal && !dryRun && !yes {
		mode := "moved to trash"
		if force {
			mode = "permanently deleted"
		}
		fmt.Printf("This will replace %d asset(s); the original will be %s after each successful replacement.\n", len(pairs), mode)
		fmt.Print("Proceed? [y/N]: ")
		if !confirm(os.Stdin) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	opts := workflows.ReplaceAssetOptions{
		DryRun:       dryRun,
		Force:        force,
		KeepOriginal: keepOriginal,
	}

	return workflows.RunBatch(pairs,
		func(p workflows.ReplacePair) string { return p.AssetID.String() },
		func(p workflows.ReplacePair) error {
			return workflows.ReplaceAsset(ctx, c, p, opts)
		},
	)
}

// confirm reads a line from r and reports whether it is "y" or "yes"
// (case-insensitive).
func confirm(r io.Reader) bool {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

// collectReplacePairs gathers asset-id/new-file-path pairs from the two
// positional arguments and/or --replace-file. At least one pair must be
// given from either source.
func collectReplacePairs(cmd *cli.Command) ([]workflows.ReplacePair, error) {
	var pairs []workflows.ReplacePair

	args := cmd.Args().Slice()
	switch len(args) {
	case 0:
		// no positional pair given; --replace-file may still supply pairs
	case 2:
		p, err := parseReplacePair(args[0] + ";" + args[1])
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, p)
	default:
		return nil, fmt.Errorf("expected exactly 2 positional arguments (ASSET_ID NEW_FILE_PATH), got %d", len(args))
	}

	if path := cmd.String("replace-file"); path != "" {
		lines, err := readReplacePairLines(path)
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			p, err := parseReplacePair(line)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, p)
		}
	}

	if len(pairs) == 0 {
		return nil, fmt.Errorf("no asset-id/new-file-path pairs given: pass them as arguments or via --replace-file")
	}
	return pairs, nil
}

// parseReplacePair parses one "assetId;newFilePath" line.
func parseReplacePair(line string) (workflows.ReplacePair, error) {
	idPart, pathPart, ok := strings.Cut(line, ";")
	if !ok {
		return workflows.ReplacePair{}, fmt.Errorf("invalid pair %q: expected format assetId;newFilePath", line)
	}
	idPart = strings.TrimSpace(idPart)
	pathPart = strings.TrimSpace(pathPart)
	if pathPart == "" {
		return workflows.ReplacePair{}, fmt.Errorf("invalid pair %q: missing new file path", line)
	}

	id, err := uuid.Parse(idPart)
	if err != nil {
		return workflows.ReplacePair{}, fmt.Errorf("invalid asset ID %q: %w", idPart, err)
	}
	return workflows.ReplacePair{AssetID: id, NewFilePath: pathPart}, nil
}

// readReplacePairLines reads one "assetId;newFilePath" line per line from
// path ("-" = stdin). Blank lines and comment lines (starting with '#' or
// '//') are skipped.
func readReplacePairLines(path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("opening replace-file: %w", err)
		}
		defer f.Close()
		r = f
	}

	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if isBlankOrComment(line) {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading replace-file %q: %w", path, err)
	}
	return lines, nil
}

func fixAlbumDatesCommand() *cli.Command {
	return &cli.Command{
		Name:  "fix-album-dates",
		Usage: "Check assets in date-named albums against the date implied by the album name, and offer to fix mismatches",
		Description: "Scans every album whose name matches \"yyyy-MM-dd <title>\" (e.g. \"2025-07-04 Garten\") or " +
			"\"yyyy <title>\" (e.g. \"2010 USA\"), fetches its assets, and flags any whose local capture date falls " +
			"outside the range implied by the name. For exact-day albums, offers to fix mismatches by setting the " +
			"asset's date to the album's date (keeping the original time of day). Year-only albums have no single " +
			"unambiguous date to fix to, so their mismatches are reported only, never changed.\n" +
			"This is the one deliberate exception to this project's rule against deprecated endpoints: fixing " +
			"uses PUT /assets/{id} (updateAsset), the only Immich API that can set an asset's capture date, which " +
			"upstream marks deprecated with no working replacement.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "print the planned date fixes without changing anything",
			},
			&cli.IntFlag{
				Name:  "offset-days",
				Usage: "allow assets up to this many days before/after the album's date before flagging them (absorbs camera timezone/DST boundary slack)",
				Value: workflows.DefaultOffsetDays,
			},
			&cli.BoolFlag{
				Name:    "interactive",
				Aliases: []string{"i"},
				Usage:   "ask once per album (y/N) instead of one bulk confirmation; --yes overrides this and skips all prompts",
			},
			&cli.BoolFlag{
				Name:  "yes",
				Usage: "skip the confirmation prompt(s) before fixing dates",
			},
		},
		Action: clientWorkflowFixAlbumDates,
	}
}

func clientWorkflowFixAlbumDates(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() > 0 {
		return fmt.Errorf("fix-album-dates takes no positional arguments (got %v)", cmd.Args().Slice())
	}

	dryRun := cmd.Bool("dry-run")
	interactive := cmd.Bool("interactive")
	yes := cmd.Bool("yes")

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	opts := workflows.FixAlbumDatesOptions{DryRun: dryRun, OffsetDays: int(cmd.Int("offset-days"))}

	checks, err := workflows.CheckAlbumDates(ctx, c, opts)
	if err != nil {
		return err
	}

	if fixableCount(checks) == 0 {
		printAlbumDateChecks(checks, c.Server, opts.OffsetDays) // still show any report-only issues
		fmt.Println("Nothing to fix.")
		return nil
	}

	fmt.Println("Note: fixing uses the deprecated PUT /assets/{id} endpoint — Immich has not shipped a replacement for setting an asset's capture date.")

	// --interactive shows, confirms, and applies one album at a time (rather
	// than batching every fix to the end) so a prompt never refers back to
	// details scrolled off-screen, and Ctrl+C at any point can never lose an
	// already-confirmed album — only albums not yet reached stay unfixed
	// (rerun the command to pick them up). --yes always wins and skips every
	// prompt (bulk or per-album).
	if interactive && !yes {
		return reviewAndFixAlbumDatesInteractively(ctx, os.Stdin, os.Stdout, checks, c.Server, opts,
			func(ctx context.Context, checks []workflows.AlbumDateCheck, opts workflows.FixAlbumDatesOptions) error {
				return workflows.FixAlbumDates(ctx, c, checks, opts)
			})
	}

	fixable := printAlbumDateChecks(checks, c.Server, opts.OffsetDays)
	if !dryRun && !yes {
		fmt.Printf("⚠️  This changes %d asset(s) automatically, without reviewing each one individually — consider --interactive or --dry-run instead.\n", fixable)
		fmt.Printf("Really change all %d asset(s) now? [y/N]: ", fixable)
		if !confirm(os.Stdin) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	return workflows.FixAlbumDates(ctx, c, checks, opts)
}

// printAlbumDateCheck prints one album's report block to w: header line, web
// UI link, and one line per mismatched asset showing its current
// LocalDateTime and, for day-precision albums, the date it would be fixed
// to.
func printAlbumDateCheck(w io.Writer, check workflows.AlbumDateCheck, server string, offsetDays int) {
	fmt.Fprintf(w, "%s (%s)  pattern=%s  range=%s..%s (\u00b1%d day(s))\n",
		check.Album.AlbumName, check.Album.Id, check.Pattern.Kind,
		check.Pattern.From.Format("2006-01-02"), check.Pattern.To.Format("2006-01-02"), offsetDays)
	fmt.Fprintf(w, "%s/albums/%s\n", server, check.Album.Id)
	for _, a := range check.Mismatches {
		if check.Pattern.Kind == workflows.PatternDay {
			fixed, _ := workflows.ComputeFixedDateTime(a, check.Pattern)
			fmt.Fprintf(w, "  %s  %s  local=%s  -> %s\n", a.Id, a.OriginalFileName, a.LocalDateTime.Format("2006-01-02 15:04:05"), fixed.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Fprintf(w, "  %s  %s  local=%s  (report only, no automatic fix)\n", a.Id, a.OriginalFileName, a.LocalDateTime.Format("2006-01-02 15:04:05"))
		}
	}
}

// reviewAndFixAlbumDatesInteractively shows and asks about one album at a
// time (via in/out) and, on "y", applies that album's fix immediately via
// fix (workflows.FixAlbumDates in production; tests inject a fake) before
// moving on — rather than collecting decisions and fixing everything only
// after the whole review finishes. This means Ctrl+C (or closed/short piped
// stdin) at any point can never lose an already-confirmed album; only
// albums not yet reached stay unfixed. Year-only albums are shown (report
// only, see workflows.ComputeFixedDateTime) without asking. A failed fix is
// reported and the review continues to the next album (same continue-on-
// error convention as workflows.RunBatch); the returned error summarizes
// how many albums failed, or nil if every attempted fix succeeded.
func reviewAndFixAlbumDatesInteractively(
	ctx context.Context,
	in io.Reader, out io.Writer,
	checks []workflows.AlbumDateCheck,
	server string, opts workflows.FixAlbumDatesOptions,
	fix func(ctx context.Context, checks []workflows.AlbumDateCheck, opts workflows.FixAlbumDatesOptions) error,
) error {
	scanner := bufio.NewScanner(in)
	fixed, failed := 0, 0

	for _, check := range checks {
		if len(check.Mismatches) == 0 {
			continue
		}
		printAlbumDateCheck(out, check, server, opts.OffsetDays)

		if check.Pattern.Kind != workflows.PatternDay {
			continue
		}

		fmt.Fprintf(out, "Fix %d asset(s) in %q? [y/N]: ", len(check.Mismatches), check.Album.AlbumName)
		if !scanner.Scan() {
			break
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer != "y" && answer != "yes" {
			continue
		}

		if err := fix(ctx, []workflows.AlbumDateCheck{check}, opts); err != nil {
			fmt.Fprintf(out, "Error: %s: %v\n", check.Album.AlbumName, err)
			failed++
			continue
		}
		fixed += len(check.Mismatches)
	}

	fmt.Fprintf(out, "--- %d asset(s) fixed ---\n", fixed)
	if failed > 0 {
		return fmt.Errorf("%d album(s) failed to fix", failed)
	}
	return nil
}

// fixableCount returns the number of day-precision mismatches across checks
// (the same count printAlbumDateChecks reports, without re-printing).
func fixableCount(checks []workflows.AlbumDateCheck) int {
	n := 0
	for _, check := range checks {
		if check.Pattern.Kind == workflows.PatternDay {
			n += len(check.Mismatches)
		}
	}
	return n
}

// printAlbumDateChecks prints a report of every album that had at least one
// date mismatch (worst-deviation album first, see workflows.CheckAlbumDates)
// and returns the number of mismatches that a fix can actually be offered
// for (day-precision albums only; year-precision mismatches are report-only
// and are printed but not counted).
func printAlbumDateChecks(checks []workflows.AlbumDateCheck, server string, offsetDays int) int {
	withIssues := 0
	fixable := 0
	for _, check := range checks {
		if len(check.Mismatches) == 0 {
			continue
		}
		withIssues++
		printAlbumDateCheck(os.Stdout, check, server, offsetDays)
		if check.Pattern.Kind == workflows.PatternDay {
			fixable += len(check.Mismatches)
		}
	}

	fmt.Printf("--- %d date-pattern album(s) scanned, %d with issue(s), %d asset(s) fixable ---\n", len(checks), withIssues, fixable)
	return fixable
}

func findNoThumbhashCommand() *cli.Command {
	return &cli.Command{
		Name:  "find-no-thumbhash",
		Usage: "Find assets that have no thumbhash (likely corrupt or unprocessed)",
		Description: "Scans all assets matching optional pre-filters and reports those whose " +
			"thumbhash is null or empty. Assets without a thumbhash often have corrupt files " +
			"that Immich could not generate a thumbnail for.\n\n" +
			"This is a non-destructive, read-only workflow — it only reports IDs.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "original-file-name",
				Aliases: []string{"n"},
				Usage:   "pre-filter by original file name (substring match)",
			},
			&cli.StringFlag{
				Name:  "type",
				Usage: "pre-filter by asset type: IMAGE, VIDEO, AUDIO, OTHER",
			},
			&cli.StringFlag{
				Name:  "album-id",
				Usage: "restrict the scan to assets in this album `UUID`",
			},
			&cli.IntFlag{
				Name:  "page-size",
				Usage: "number of assets per API page (max 1000)",
				Value: 250,
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "print results as JSON array",
			},
			&cli.BoolFlag{
				Name:    "ids-only",
				Aliases: []string{"q"},
				Usage:   "print only asset IDs, one per line",
			},
		},
		Action: clientWorkflowFindNoThumbhash,
	}
}

func clientWorkflowFindNoThumbhash(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	opts := workflows.FindNoThumbhashOptions{
		PageSize:         cmd.Int("page-size"),
		OriginalFileName: cmd.String("original-file-name"),
		Type:             cmd.String("type"),
	}

	if albumIDStr := cmd.String("album-id"); albumIDStr != "" {
		albumID, perr := uuid.Parse(albumIDStr)
		if perr != nil {
			return fmt.Errorf("invalid --album-id %q: %w", albumIDStr, perr)
		}
		opts.AlbumIDs = []openapi_types.UUID{albumID}
	}

	results, err := workflows.FindAssetsWithNoThumbhash(ctx, c, opts)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println("No assets without thumbhash found.")
		return nil
	}

	switch {
	case cmd.Bool("json"):
		raw, jsonErr := json.MarshalIndent(results, "", "  ")
		if jsonErr != nil {
			return fmt.Errorf("marshalling results: %w", jsonErr)
		}
		fmt.Println(string(raw))

	case cmd.Bool("ids-only"):
		for _, a := range results {
			fmt.Println(a.ID)
		}

	default:
		fmt.Printf("Found %d asset(s) without thumbhash:\n", len(results))
		for _, a := range results {
			fmt.Printf("%s\t%s\t%s\n", a.ID, a.OriginalFileName, a.Type)
		}
	}

	return nil
}

func findHEICTileDefectCommand() *cli.Command {
	return &cli.Command{
		Name:  "find-heic-tile-defect",
		Usage: "Find HEIC/HEIF assets whose dimensions trigger a known Immich thumbnail-corruption defect",
		Description: "Scans all IMAGE assets matching optional pre-filters and reports the HEIC/HEIF " +
			"ones whose pixel width or height is not an exact multiple of the HEIF grid tile size " +
			"(512px by default). HEIC/HEIF files store large images as a grid of tiles; when the " +
			"image dimensions aren't an exact multiple of the tile size the last row/column of " +
			"tiles must be cropped, and Immich's thumbnailer (libvips/libheif) has been observed to " +
			"render a garbled, low-detail preview/thumbnail for such files instead of the real " +
			"image — confirmed by independently decoding several affected assets, where the " +
			"original bytes are completely fine. HEIC produced by desktop conversion tools (e.g. " +
			"Zoner Photo Studio X, DxO PhotoLab) reliably hits this because they tile at a fixed " +
			"default size regardless of the source image's dimensions; camera/phone-native HEIC " +
			"tends to avoid it.\n\n" +
			"This is a structural heuristic based on dimensions alone, not a guaranteed defect — " +
			"treat matches as candidates for manual/visual confirmation (e.g. compare the asset's " +
			"thumbnail in the web UI against the downloaded original) before replacing or deleting " +
			"anything. By default this is a non-destructive, read-only workflow — it only reports " +
			"assets. Pass --apply-tag to additionally tag every found candidate with --tag (creating " +
			"the tag, and any missing parent tags, if needed) so they can be found again later in the " +
			"web UI. Fix candidates by re-exporting/re-converting the source file (e.g. without HEIC " +
			"tiling, or as JPEG) and re-importing it with 'client-workflow replace-asset'.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "original-file-name",
				Aliases: []string{"n"},
				Usage:   "pre-filter by original file name (substring match)",
			},
			&cli.StringFlag{
				Name:  "album-id",
				Usage: "restrict the scan to assets in this album `UUID`",
			},
			&cli.IntFlag{
				Name:  "page-size",
				Usage: "number of assets per API page (max 1000)",
				Value: 250,
			},
			&cli.IntFlag{
				Name:  "tile-size",
				Usage: "assumed HEIF grid tile size in pixels",
				Value: 512,
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "print results as JSON array",
			},
			&cli.BoolFlag{
				Name:    "ids-only",
				Aliases: []string{"q"},
				Usage:   "print only asset IDs, one per line",
			},
			&cli.BoolFlag{
				Name:  "apply-tag",
				Usage: "tag every found candidate asset with --tag (default: report only, no tagging)",
			},
			&cli.StringFlag{
				Name:  "tag",
				Usage: "tag value (full path) applied to found candidates when --apply-tag is set",
				Value: "immich-admin-cli/corrupt-heic",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "with --apply-tag, print what would be tagged without changing anything",
			},
		},
		Action: clientWorkflowFindHEICTileDefect,
	}
}

func clientWorkflowFindHEICTileDefect(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	opts := workflows.FindHEICTileDefectOptions{
		PageSize:         cmd.Int("page-size"),
		TileSize:         cmd.Int("tile-size"),
		OriginalFileName: cmd.String("original-file-name"),
	}

	if albumIDStr := cmd.String("album-id"); albumIDStr != "" {
		albumID, perr := uuid.Parse(albumIDStr)
		if perr != nil {
			return fmt.Errorf("invalid --album-id %q: %w", albumIDStr, perr)
		}
		opts.AlbumIDs = []openapi_types.UUID{albumID}
	}

	results, err := workflows.FindAssetsWithHEICTileDefect(ctx, c, opts)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println("No HEIC tile-defect candidates found.")
		return nil
	}

	switch {
	case cmd.Bool("json"):
		raw, jsonErr := json.MarshalIndent(results, "", "  ")
		if jsonErr != nil {
			return fmt.Errorf("marshalling results: %w", jsonErr)
		}
		fmt.Println(string(raw))

	case cmd.Bool("ids-only"):
		for _, a := range results {
			fmt.Println(a.ID)
		}

	default:
		fmt.Printf("Found %d HEIC tile-defect candidate(s) (verify visually before acting):\n", len(results))
		for _, a := range results {
			fmt.Printf("%s\t%dx%d\t%s\n", a.ID, a.Width, a.Height, a.OriginalFileName)
		}
	}

	if !cmd.Bool("apply-tag") {
		return nil
	}

	tagValue := cmd.String("tag")
	if tagValue == "" {
		return fmt.Errorf("--tag must not be empty when --apply-tag is set")
	}

	if cmd.Bool("dry-run") {
		fmt.Printf("[dry-run] would tag %d asset(s) with %q\n", len(results), tagValue)
		return nil
	}

	ids := make([]openapi_types.UUID, len(results))
	for i, a := range results {
		id, perr := uuid.Parse(a.ID)
		if perr != nil {
			return fmt.Errorf("parsing asset ID %q: %w", a.ID, perr)
		}
		ids[i] = id
	}

	tagID, err := workflows.ResolveOrCreateTag(ctx, c, tagValue)
	if err != nil {
		return err
	}
	if err := workflows.TagAssets(ctx, c, ids, tagID); err != nil {
		return err
	}
	fmt.Printf("Tagged %d asset(s) with %q (tag id %s)\n", len(ids), tagValue, tagID)

	return nil
}

func repairAssetsCommand() *cli.Command {
	return &cli.Command{
		Name:      "repair-assets",
		Usage:     "Repair corrupt image assets and re-import them, keeping metadata",
		ArgsUsage: "[ASSET_ID ...]",
		Description: "Repairs corrupt JPEG and TIFF assets in selectable modes and re-imports each fixed " +
			"file via the replace-asset flow (upload → checksum verify → copy metadata → " +
			"remove original).\n\n" +
			"Modes:\n" +
			"  marker        append the missing JPEG End-of-Image marker (FF D9). Lossless, EXIF preserved.\n" +
			"  tiff-tags     patch TIFF IFD entries with an invalid zero count field (the exact defect " +
			"libtiff rejects as \"Null count for Tag N\", breaking Immich's thumbnailer). Lossless: " +
			"only the 4-byte count field of each offending entry is changed, pixel data and all " +
			"metadata (EXIF/XMP/dates) are untouched.\n" +
			"  takeout-json  DELETE mode (not a repair): find assets whose bytes are actually a Google " +
			"Photos Takeout metadata JSON sidecar imported in place of the real photo — the file has " +
			"no image data and is unrecoverable — and delete them (to trash by default; --force to " +
			"delete permanently). Detection is structural: the leading JSON object must parse AND carry " +
			"the full Google fingerprint (title + photoTakenTime.timestamp + creationTime.timestamp + " +
			"googlePhotosOrigin), so a real image can never match. Use --dry-run to only detect. This " +
			"mode is opt-in only and is NOT included in 'all'.\n" +
			"  all           run every safe repair strategy across both file types (currently marker + " +
			"tiff-tags). Excludes the destructive takeout-json mode.\n\n" +
			"When a repair mode (marker/tiff-tags/all) encounters an asset that is actually a Google " +
			"Takeout JSON sidecar rather than a repairable image, it reports it as unrepairable and " +
			"prints a hint to rerun that asset with --mode takeout-json to delete it.\n\n" +
			"Detection is structural, not a blind per-file-type guess: tiff-tags only applies when the " +
			"file's IFD chain parses cleanly AND at least one entry has the invalid zero count field — " +
			"a TIFF without that specific defect is reported as already-ok, never modified.\n\n" +
			"Because the repaired bytes differ, the fix is a re-import: the repaired file is " +
			"uploaded as a new asset, its checksum is verified, the original's metadata is copied " +
			"onto it, and only then is the original removed. Removal is NOT gated on Immich having " +
			"generated a thumbhash for the new asset yet — thumbnail generation is asynchronous and " +
			"its timing depends on too many server-side factors (queue depth, load, job scheduling) " +
			"to reliably wait for. If an earlier step (upload or checksum verify or metadata copy) " +
			"fails, the original is left untouched and the failed upload is rolled back (trashed). " +
			"Use find-no-thumbhash afterwards to confirm a repair actually produced a thumbnail, or " +
			"pass --keep-original to be able to re-check before the original is gone.\n\n" +
			"Provide assets either as IDs (positional and/or --ids-file) OR via --check-all-assets " +
			"(which scans every IMAGE asset with no thumbhash — the usual corruption signature). " +
			"Assets with no applicable strategy for the chosen mode (e.g. non-JPEG/TIFF files, or " +
			"RAW/DNG variants such as JPEG-XL-compressed DNGs) are skipped, not modified.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "mode",
				Usage: "repair mode: all | marker | tiff-tags | takeout-json",
				Value: "all",
			},
			idsFileFlag(),
			&cli.BoolFlag{
				Name:  "check-all-assets",
				Usage: "attempt repair on every IMAGE asset with no thumbhash (cannot be combined with explicit IDs)",
			},
			&cli.StringFlag{
				Name:  "album-id",
				Usage: "attempt repair on every IMAGE asset with no thumbhash in this album `UUID` (cannot be combined with explicit IDs or --check-all-assets)",
			},
			&cli.BoolFlag{
				Name:  "keep-original",
				Usage: "repair and re-import but keep the original asset instead of removing it",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "permanently delete the original asset instead of moving it to trash",
			},
			&cli.IntFlag{
				Name:  "page-size",
				Usage: "number of assets per API page when scanning with --check-all-assets (max 1000)",
				Value: 250,
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "print the planned steps for each asset without changing anything",
			},
			&cli.BoolFlag{
				Name:  "yes",
				Usage: "skip the confirmation prompt before removing original assets",
			},
		},
		Action: clientWorkflowRepairAssets,
	}
}

func clientWorkflowRepairAssets(ctx context.Context, cmd *cli.Command) error {
	mode, err := workflows.ParseRepairMode(cmd.String("mode"))
	if err != nil {
		return err
	}

	checkAll := cmd.Bool("check-all-assets")
	albumIDStr := cmd.String("album-id")
	dryRun := cmd.Bool("dry-run")
	keepOriginal := cmd.Bool("keep-original")
	force := cmd.Bool("force")
	yes := cmd.Bool("yes")

	if mode == workflows.RepairModeTakeoutJSON && keepOriginal {
		return fmt.Errorf("--keep-original is not applicable to --mode takeout-json (the file is an unrecoverable JSON sidecar, there is nothing to keep); use --dry-run to only detect without deleting")
	}

	// Resolve the asset source: exactly one of explicit IDs (positional and/or
	// --ids-file), --check-all-assets, or --album-id.
	hasExplicit := cmd.Args().Len() > 0 || cmd.String("ids-file") != ""
	hasAlbum := albumIDStr != ""
	sources := 0
	for _, on := range []bool{hasExplicit, checkAll, hasAlbum} {
		if on {
			sources++
		}
	}
	if sources == 0 {
		return fmt.Errorf("no assets given: pass asset IDs / --ids-file, --check-all-assets, or --album-id")
	}
	if sources > 1 {
		return fmt.Errorf("choose exactly one asset source: asset IDs / --ids-file, --check-all-assets, or --album-id")
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	var ids []openapi_types.UUID
	switch {
	case checkAll:
		ids, err = repairScanNoThumbhash(ctx, c, workflows.FindNoThumbhashOptions{
			PageSize: cmd.Int("page-size"),
			Type:     "IMAGE",
		})
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			fmt.Println("No IMAGE assets without thumbhash found; nothing to repair.")
			return nil
		}
		fmt.Printf("Found %d asset(s) without thumbhash to attempt repair.\n", len(ids))

	case hasAlbum:
		albumID, perr := uuid.Parse(albumIDStr)
		if perr != nil {
			return fmt.Errorf("invalid --album-id %q: %w", albumIDStr, perr)
		}
		name, count, aerr := workflows.GetAlbumSummary(ctx, c, albumID)
		if aerr != nil {
			return aerr
		}
		fmt.Printf("Album %q (%d asset(s)); scanning for IMAGE assets without thumbhash...\n", name, count)
		ids, err = repairScanNoThumbhash(ctx, c, workflows.FindNoThumbhashOptions{
			PageSize: cmd.Int("page-size"),
			Type:     "IMAGE",
			AlbumIDs: []openapi_types.UUID{albumID},
		})
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			fmt.Println("No IMAGE assets without thumbhash found in that album; nothing to repair.")
			return nil
		}
		fmt.Printf("Found %d asset(s) without thumbhash to attempt repair.\n", len(ids))

	default:
		ids, err = collectIDs(cmd)
		if err != nil {
			return err
		}
	}

	// The removal of originals is destructive, so require confirmation unless
	// it won't run (--keep-original) or this is a dry run.
	if !keepOriginal && !dryRun && !yes {
		disposition := "moved to trash"
		if force {
			disposition = "permanently deleted"
		}
		if mode == workflows.RepairModeTakeoutJSON {
			fmt.Printf("This will scan %d asset(s); any confirmed Google Takeout JSON sidecar (a file with no recoverable image data) will be %s. Non-sidecar files are left untouched.\n", len(ids), disposition)
		} else {
			fmt.Printf("This will attempt to repair %d asset(s); each original will be %s once its repaired replacement is uploaded and verified (checksum + metadata copy), regardless of whether Immich has generated a thumbnail for it yet.\n", len(ids), disposition)
		}
		fmt.Print("Proceed? [y/N]: ")
		if !confirm(os.Stdin) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	tempDir, err := os.MkdirTemp("", "immich-repair-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	opts := workflows.RepairAssetsOptions{
		Mode:         mode,
		DryRun:       dryRun,
		Force:        force,
		KeepOriginal: keepOriginal,
		TempDir:      tempDir,
	}

	counts := map[workflows.RepairOutcome]int{}
	runErr := workflows.RunBatch(ids,
		func(id openapi_types.UUID) string { return id.String() },
		func(id openapi_types.UUID) error {
			outcome, err := workflows.RepairAsset(ctx, c, id, opts)
			if outcome != "" {
				counts[outcome]++
			}
			return err
		},
	)

	if mode == workflows.RepairModeTakeoutJSON {
		fmt.Printf("\nSummary: deleted-sidecar=%d skipped-not-sidecar=%d (of %d)\n",
			counts[workflows.OutcomeDeletedSidecar],
			counts[workflows.OutcomeSkippedUnsupported],
			len(ids),
		)
	} else {
		fmt.Printf("\nSummary: repaired=%d already-ok=%d skipped-unsupported=%d unrepairable=%d (of %d)\n",
			counts[workflows.OutcomeRepaired],
			counts[workflows.OutcomeAlreadyOK],
			counts[workflows.OutcomeSkippedUnsupported],
			counts[workflows.OutcomeUnrepairable],
			len(ids),
		)
	}

	return runErr
}

// repairScanNoThumbhash runs the no-thumbhash finder and parses the results
// into asset UUIDs for repair.
func repairScanNoThumbhash(ctx context.Context, c *client.Client, opts workflows.FindNoThumbhashOptions) ([]openapi_types.UUID, error) {
	results, err := workflows.FindAssetsWithNoThumbhash(ctx, c, opts)
	if err != nil {
		return nil, err
	}
	ids := make([]openapi_types.UUID, 0, len(results))
	for _, a := range results {
		id, err := uuid.Parse(a.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid asset ID %q from search: %w", a.ID, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// validateDownloadAlbumAlbumFlags ensures exactly one of --album-id /
// --album-name was given, extracted as a pure function so it is directly
// unit-testable without spinning up a full CLI command run.
func validateDownloadAlbumAlbumFlags(albumIDStr, albumName string) error {
	if (albumIDStr == "") == (albumName == "") {
		return fmt.Errorf("exactly one of --album-id or --album-name is required")
	}
	return nil
}

// resolveDownloadAlbumSize validates the --size flag value against the full
// AssetMediaSize enum (fullsize, original, preview, thumbnail).
// "original" is accepted here even though the OpenAPI spec deprecates
// size=original on GET /assets/{id}/thumbnail ("Use the original endpoint
// directly instead"): download-album's workflow layer special-cases
// original to the dedicated GET /assets/{id}/original endpoint (see
// fetchAssetStream) and never sends size=original to the thumbnail
// endpoint, so it is exempt from that deprecation. Contrast with
// download-thumbnail, a thin direct wrapper over the thumbnail endpoint,
// which rejects "original" for exactly this reason.
func resolveDownloadAlbumSize(raw string) (immichapi.AssetMediaSize, error) {
	size := immichapi.AssetMediaSize(raw)
	if !size.Valid() {
		return "", fmt.Errorf("invalid --size %q: must be one of %q, %q, %q, %q", raw, immichapi.Original, immichapi.Fullsize, immichapi.Preview, immichapi.Thumbnail)
	}
	return size, nil
}

func downloadAlbumCommand() *cli.Command {
	return &cli.Command{
		Name:  "download-album",
		Usage: "Download all original files or a smaller variant (preview/thumbnail/fullsize) from one album into a local folder, optionally kept in sync",
		Description: "Downloads every asset in exactly one album (--album-id or --album-name) into --target-dir, " +
			"as either the full original file or a smaller variant (--size original|fullsize|preview|thumbnail, " +
			"default preview), optionally skipping videos (--ignore-videos). Without --sync this is a plain " +
			"one-shot bulk download that always overwrites. " +
			"With --sync, a hidden manifest (.immich-album-sync.json) is kept in --target-dir to detect assets " +
			"that changed (re-downloaded) or left the album (local file deleted) on later runs; files not " +
			"tracked in the manifest are never touched. --timestamp-prefix prefixes each file name with the " +
			"asset's capture date/time for chronological sorting. --resize re-encodes every downloaded IMAGE " +
			"file to JPEG via ImageMagick, optionally resized (--resize-width/--resize-height) and at a given " +
			"quality (--resize-quality). --resize-video-preset re-encodes every downloaded VIDEO original via " +
			"ffmpeg using a named preset.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "album-id",
				Usage: "album `ID` to download (mutually exclusive with --album-name)",
			},
			&cli.StringFlag{
				Name:  "album-name",
				Usage: "album name to download; must resolve to exactly one album (mutually exclusive with --album-id)",
			},
			&cli.StringFlag{
				Name:     "target-dir",
				Usage:    "local directory to download into (created if missing)",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "size",
				Usage: "media variant to download: original, fullsize, preview, or thumbnail (see the AssetMediaSize spec enum)",
				Value: string(immichapi.Preview),
			},
			&cli.BoolFlag{
				Name:  "ignore-videos",
				Usage: "skip video assets",
			},
			&cli.BoolFlag{
				Name:  "sync",
				Usage: "keep --target-dir in sync using a manifest: skip unchanged assets, re-download changed ones, and delete local files for assets removed from the album",
			},
			&cli.BoolFlag{
				Name:  "timestamp-prefix",
				Usage: "prefix each local file name with the asset's capture date/time (\"yyyy-MM-dd_HH_mm_ss\", from its metadata)",
			},
			&cli.BoolFlag{
				Name:  "resize",
				Usage: "resize/re-encode every downloaded file to JPEG using ImageMagick (path from config tools.imagemagick_path or IMMICH_IMAGEMAGICK_PATH, falling back to PATH)",
			},
			&cli.IntFlag{
				Name:  "resize-width",
				Usage: "target width in pixels; 0 = unconstrained on that axis (requires --resize)",
			},
			&cli.IntFlag{
				Name:  "resize-height",
				Usage: "target height in pixels; 0 = unconstrained on that axis (requires --resize)",
			},
			&cli.IntFlag{
				Name:  "resize-quality",
				Usage: "JPEG quality 1-100 (requires --resize)",
				Value: workflows.DefaultResizeQuality,
			},
			&cli.StringFlag{
				Name:  "resize-video-preset",
				Usage: fmt.Sprintf("re-encode every downloaded VIDEO asset using ffmpeg (path from config tools.ffmpeg_path or IMMICH_FFMPEG_PATH, falling back to PATH); videos are always fetched at --size original for this regardless of --size (many videos have no usable preview/thumbnail rendition, and ffmpeg needs the real stream anyway) — only non-video assets use --size; valid presets: %s", strings.Join(workflows.ValidResizeVideoPresets, ", ")),
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "print the planned downloads/deletions without changing anything",
			},
			&cli.BoolFlag{
				Name:  "yes",
				Usage: "skip the confirmation prompt before deleting local files (--sync only)",
			},
		},
		Action: clientWorkflowDownloadAlbum,
	}
}

// validateResizeVideoPreset checks --resize-video-preset against
// workflows.ValidResizeVideoPresets without touching the filesystem or
// environment (resolving ffmpeg is a separate, later step), so it's
// directly unit-testable. An empty raw value means the feature is disabled
// (valid).
func validateResizeVideoPreset(raw string) error {
	if raw == "" {
		return nil
	}
	if slices.Contains(workflows.ValidResizeVideoPresets, raw) {
		return nil
	}
	return fmt.Errorf("invalid --resize-video-preset %q: valid presets: %s", raw, strings.Join(workflows.ValidResizeVideoPresets, ", "))
}

// validateResizeFlags checks --resize-width/--resize-height/--resize-quality
// without touching the filesystem or environment (resolving the ImageMagick
// executable is a separate, later step), so it's directly unit-testable.
func validateResizeFlags(enabled bool, widthSet, heightSet, qualitySet bool, width, height, quality int) error {
	if !enabled {
		if widthSet || heightSet || qualitySet {
			return fmt.Errorf("--resize-width/--resize-height/--resize-quality require --resize")
		}
		return nil
	}
	if width < 0 || height < 0 {
		return fmt.Errorf("--resize-width/--resize-height must not be negative")
	}
	if quality < 1 || quality > 100 {
		return fmt.Errorf("invalid --resize-quality %d: must be between 1 and 100", quality)
	}
	return nil
}

func clientWorkflowDownloadAlbum(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() > 0 {
		return fmt.Errorf("download-album takes no positional arguments; pass --album-id/--album-name and --target-dir (got %v)", cmd.Args().Slice())
	}

	albumIDStr := cmd.String("album-id")
	albumName := cmd.String("album-name")
	if err := validateDownloadAlbumAlbumFlags(albumIDStr, albumName); err != nil {
		return err
	}

	targetDir := cmd.String("target-dir")

	size, err := resolveDownloadAlbumSize(cmd.String("size"))
	if err != nil {
		return err
	}

	resizeEnabled := cmd.Bool("resize")
	resizeWidth := int(cmd.Int("resize-width"))
	resizeHeight := int(cmd.Int("resize-height"))
	resizeQuality := int(cmd.Int("resize-quality"))
	if err := validateResizeFlags(resizeEnabled, cmd.IsSet("resize-width"), cmd.IsSet("resize-height"), cmd.IsSet("resize-quality"), resizeWidth, resizeHeight, resizeQuality); err != nil {
		return err
	}

	resizeOpts := workflows.ResizeOptions{Enabled: resizeEnabled, Width: resizeWidth, Height: resizeHeight, Quality: resizeQuality}
	if resizeEnabled {
		// Resolved once, before any download starts (fail fast), per the
		// project's convention for external local-processing tools.
		cfg, err := config.Load(cmd.String("config"))
		if err != nil {
			return err
		}
		execPath, err := workflows.ResolveImageMagickPath(cfg.Tools.ImageMagickPath)
		if err != nil {
			return err
		}
		resizeOpts.ExecutablePath = execPath
	}

	resizeVideoPreset := cmd.String("resize-video-preset")
	if err := validateResizeVideoPreset(resizeVideoPreset); err != nil {
		return err
	}
	resizeVideoOpts := workflows.ResizeVideoOptions{Enabled: resizeVideoPreset != "", Preset: resizeVideoPreset}
	if resizeVideoOpts.Enabled {
		// Resolved once, before any download starts (fail fast), same as
		// ImageMagick above.
		cfg, err := config.Load(cmd.String("config"))
		if err != nil {
			return err
		}
		execPath, err := workflows.ResolveFFmpegPath(cfg.Tools.FFmpegPath)
		if err != nil {
			return err
		}
		resizeVideoOpts.ExecutablePath = execPath
	}

	var albumID *openapi_types.UUID
	if albumIDStr != "" {
		id, err := uuid.Parse(albumIDStr)
		if err != nil {
			return fmt.Errorf("invalid --album-id %q: %w", albumIDStr, err)
		}
		uid := openapi_types.UUID(id)
		albumID = &uid
	}

	dryRun := cmd.Bool("dry-run")
	sync := cmd.Bool("sync")
	yes := cmd.Bool("yes")

	opts := workflows.DownloadAlbumOptions{
		Size:            size,
		IgnoreVideos:    cmd.Bool("ignore-videos"),
		DryRun:          dryRun,
		Resize:          resizeOpts,
		ResizeVideo:     resizeVideoOpts,
		TimestampPrefix: cmd.Bool("timestamp-prefix"),
	}

	// Printed before any network activity (newClient/ResolveAlbum below),
	// so the user always sees it up front, even if the run later fails to
	// reach the server.
	printDownloadSizePlan(opts)

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	album, err := workflows.ResolveAlbum(ctx, c, albumID, albumName)
	if err != nil {
		return err
	}

	if !sync {
		return workflows.DownloadAlbum(ctx, c, album, targetDir, opts)
	}

	assets, plan, manifest, err := workflows.PlanAlbumSync(ctx, c, album, targetDir, opts)
	if err != nil {
		return err
	}

	printDownloadAlbumSyncPlan(album, plan)

	if len(plan.Additions) == 0 && len(plan.Updates) == 0 && len(plan.Removals) == 0 {
		fmt.Println("Already up to date.")
		return nil
	}

	if dryRun {
		return nil
	}

	if len(plan.Removals) > 0 && !yes {
		fmt.Printf("This will permanently delete %d local file(s) whose asset is no longer in the album. Continue? [y/N]: ", len(plan.Removals))
		if !confirm(os.Stdin) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	return workflows.ApplyAlbumSync(ctx, c, album, targetDir, assets, plan, manifest, opts)
}

// printDownloadSizePlan prints a short, upfront summary of which media
// variant each asset kind will be downloaded as for a download-album run,
// so it's clear before any network activity starts — in particular that
// VIDEO assets always use --size original when --resize-video-preset is
// set (see effectiveSize in the workflows package for why), regardless of
// --size for every other asset. Printed once per run, for both plain and
// --sync mode, and regardless of --dry-run.
func printDownloadSizePlan(opts workflows.DownloadAlbumOptions) {
	fmt.Printf("Download plan: --size %s for photo/other assets\n", opts.Size)
	switch {
	case opts.ResizeVideo.Enabled:
		fmt.Printf("  videos: downloaded as %s and re-encoded to MP4 via --resize-video-preset %s (regardless of --size above)\n", immichapi.Original, opts.ResizeVideo.Preset)
	case opts.Size != immichapi.Original:
		fmt.Printf("  videos: downloaded as a static preview image (--size %s), NOT the real video — pass --size original or --resize-video-preset to get actual video content\n", opts.Size)
	}
	if opts.Resize.Enabled {
		fmt.Printf("  images: re-encoded to JPEG via --resize (quality %d)\n", opts.Resize.Quality)
	}
}

// printDownloadAlbumSyncPlan prints a summary of a `download-album --sync`
// run's planned actions (used for both --dry-run previews and the real run).
func printDownloadAlbumSyncPlan(album immichapi.AlbumResponseDto, plan workflows.SyncPlan) {
	fmt.Printf("Album %q (%s): %d to add, %d to update, %d unchanged, %d to remove\n",
		album.AlbumName, album.Id, len(plan.Additions), len(plan.Updates), len(plan.Unchanged), len(plan.Removals))
	for _, a := range plan.Additions {
		fmt.Printf("  + %s (%s)\n", a.OriginalFileName, a.Id)
	}
	for _, a := range plan.Updates {
		fmt.Printf("  ~ %s (%s)\n", a.OriginalFileName, a.Id)
	}
	for _, r := range plan.Removals {
		fmt.Printf("  - %s (%s)\n", r.FileName, r.AssetID)
	}
}
