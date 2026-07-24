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
	"github.com/urfave/cli/v3"

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
		Commands: []*cli.Command{
			replaceAssetCommand(),
			tagDeleteCommand(),
			findNoThumbhashCommand(),
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
