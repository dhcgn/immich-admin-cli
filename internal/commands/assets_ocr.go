package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// assetsOcrCommand exposes GET /assets/{id}/ocr (getAssetOcr), also driven by
// `client-workflow date-from-ocr`.
func assetsOcrCommand() *cli.Command {
	return &cli.Command{
		Name:      "ocr",
		Usage:     "Show the OCR text boxes Immich recognized in one or more assets (GET /assets/{id}/ocr)",
		ArgsUsage: "[ASSET_ID|ASSET_URL ...]",
		Flags: []cli.Flag{
			idsFileFlag(),
			&cli.BoolFlag{
				Name:  "json",
				Usage: "print {assetId: [boxes]} as JSON (text, textScore, boxScore, normalized x1..y4)",
			},
		},
		Action: assetsOcr,
	}
}

func assetsOcr(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	all := map[string][]immichapi.AssetOcrResponseDto{}
	failures := 0
	for _, id := range ids {
		boxes, err := workflows.FetchAssetOCR(ctx, c, id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: asset %s: %v\n", id, err)
			failures++
			continue
		}
		all[id.String()] = boxes
		if !cmd.Bool("json") {
			fmt.Printf("%s  %d box(es)\n", id, len(boxes))
			for _, b := range boxes {
				fmt.Printf("  text=%.2f box=%.2f  at x=%.2f y=%.2f  %q\n", b.TextScore, b.BoxScore, b.X1, b.Y1, b.Text)
			}
		}
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(all); err != nil {
			return err
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d assets failed", failures, len(ids))
	}
	return nil
}
