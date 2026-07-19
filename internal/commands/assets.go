package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// Assets returns the `assets` command group (spec tag: Assets).
func Assets() *cli.Command {
	return &cli.Command{
		Name:  "assets",
		Usage: "Asset operations",
		Commands: []*cli.Command{
			{
				Name:      "info",
				Usage:     "Show information about one or more assets (GET /assets/{id})",
				ArgsUsage: "[ASSET_ID ...]",
				Flags: []cli.Flag{
					idsFileFlag(),
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the raw responses as a JSON array",
					},
				},
				Action: assetsInfo,
			},
			assetsDownloadCommand(),
		},
	}
}

func assetsInfo(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(cmd)
	if err != nil {
		return err
	}

	// Fan out over the single-asset endpoint: continue on per-ID errors and
	// report failures at the end.
	var assets []*immichapi.AssetResponseDto
	failures := 0
	for _, id := range ids {
		resp, err := c.API.GetAssetInfoWithResponse(ctx, id, nil)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: asset %s: %v\n", id, err)
			failures++
			continue
		}
		assets = append(assets, resp.JSON200)
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(assets); err != nil {
			return err
		}
	} else {
		for i, a := range assets {
			if i > 0 {
				fmt.Println()
			}
			printAsset(a)
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d assets failed", failures, len(ids))
	}
	return nil
}

func printAsset(a *immichapi.AssetResponseDto) {
	fmt.Printf("ID:        %s\n", a.Id)
	fmt.Printf("Name:      %s\n", a.OriginalFileName)
	fmt.Printf("Type:      %s\n", a.Type)
	if a.ExifInfo != nil {
		fmt.Printf("Size:      %s\n", formatBytes(a.ExifInfo.FileSizeInByte))
	}
	if a.Width != nil && a.Height != nil {
		fmt.Printf("Dimension: %dx%d\n", *a.Width, *a.Height)
	}
	fmt.Printf("Captured:  %s\n", a.FileCreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Path:      %s\n", a.OriginalPath)
	fmt.Printf("Flags:     favorite=%t archived=%t trashed=%t offline=%t\n",
		a.IsFavorite, a.IsArchived, a.IsTrashed, a.IsOffline)
}
