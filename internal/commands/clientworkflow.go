package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
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
