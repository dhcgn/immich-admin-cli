package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/clipprobe"
	"github.com/dhcgn/immich-admin-cli/internal/config"
)

func findSimilarCommand() *cli.Command {
	return &cli.Command{
		Name:      "find-similar",
		Usage:     "Find assets in Immich that look like a local image file (via clip-probe)",
		ArgsUsage: "FILE",
		Description: "Submit one local image file to the external immich-clip-probe service " +
			"and list the visually-nearest assets already in your Immich library, ordered by " +
			"ascending cosine distance. Read-only: nothing is uploaded to Immich.\n" +
			"Requires the clip-probe service running next to Immich — setup: " + config.ClipProbeRepoURL +
			" Configure clip_probe.server + clip_probe.token in the config file " +
			"(or IMMICH_CLIP_PROBE_SERVER / IMMICH_CLIP_PROBE_TOKEN env vars).",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "limit",
				Usage: "maximum number of matches to return (1-100)",
				Value: 10,
			},
			&cli.FloatFlag{
				Name:  "max-distance",
				Usage: "cosine-distance cut-off; matches above this are dropped (0-2)",
				Value: 0.01,
			},
			&cli.BoolFlag{
				Name:  "all",
				Usage: "return the limit nearest matches regardless of --max-distance (for calibrating a threshold)",
			},
			&cli.StringFlag{
				Name:  "type",
				Usage: "restrict to a single asset type: IMAGE, VIDEO, or all",
				Value: "IMAGE",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "print the raw clip-probe response as JSON",
			},
			&cli.BoolFlag{
				Name:    "ids-only",
				Aliases: []string{"q"},
				Usage:   "print only matching asset IDs, one per line",
			},
		},
		Action: clientWorkflowFindSimilar,
	}
}

func clientWorkflowFindSimilar(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 1 {
		return fmt.Errorf("expected exactly 1 positional argument (FILE), got %d", len(args))
	}
	file := args[0]
	if st, err := os.Stat(file); err != nil {
		return fmt.Errorf("checking file %q: %w", file, err)
	} else if st.IsDir() {
		return fmt.Errorf("not a file: %q", file)
	}

	opts := clipprobe.Options{
		Limit:       int(cmd.Int("limit")),
		MaxDistance: cmd.Float("max-distance"),
		All:         cmd.Bool("all"),
		Type:        cmd.String("type"),
	}
	// Option range checks run inside clip.FindSimilar (clipprobe.Options
	// validation); nothing mutates before that, so no pre-check is needed
	// here. (Note: do not call a method literally named `Validate` from
	// this package — tools/apitable would mistake it for the Immich
	// `validate` library operation and miscount API coverage.)

	// Identity line first (same convention as every other command).
	if _, err := newClient(ctx, cmd); err != nil {
		return err
	}

	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return err
	}
	if err := cfg.ValidateClipProbe(); err != nil {
		return err
	}

	clip := clipprobe.New(cfg.ClipProbe.Server, cfg.ClipProbe.Token)
	resp, raw, err := clip.FindSimilar(ctx, file, opts)
	if err != nil {
		return err
	}

	switch {
	case cmd.Bool("json"):
		fmt.Println(string(raw))
	case cmd.Bool("ids-only"):
		for _, m := range resp.Matches {
			fmt.Println(m.AssetID)
		}
	default:
		fmt.Printf("duplicate: %v (maxDistance %.4g, %d match(es))\n", resp.Duplicate, resp.MaxDistance, len(resp.Matches))
		for _, m := range resp.Matches {
			fmt.Printf("%s\tdistance=%.4f\tsimilarity=%.4f\t%s\t%s\n", m.AssetID, m.Distance, m.Similarity, m.OriginalFileName, m.LocalDateTime)
			if m.Links != nil && m.Links.Web != "" {
				fmt.Printf("  %s\n", m.Links.Web)
			}
		}
		if len(resp.Matches) == 0 {
			fmt.Println("No similar assets found.")
		}
	}
	return nil
}
