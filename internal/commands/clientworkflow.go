package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
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
			findNoThumbhashCommand(),
			repairAssetsCommand(),
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

	c, err := newClient(cmd)
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

	c, err := newClient(cmd)
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
	c, err := newClient(cmd)
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

	c, err := newClient(cmd)
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
