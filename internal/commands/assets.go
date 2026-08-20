package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
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
			assetsUpdateCommand(),
			assetsDownloadCommand(),
			assetsDownloadThumbnailCommand(),
		},
	}
}

func assetsInfo(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
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

// assetsUpdateCommand exposes PUT /assets/{id} (updateAsset). Kept as a
// standalone command per project convention (a client workflow driving an
// operation also ships the raw operation), used by `client-workflow
// fix-album-dates` to set an asset's capture date.
//
// updateAsset is marked deprecated upstream with a self-referential (i.e.
// non-existent) replacementId — no stable alternative exists for setting an
// asset's capture date, so this is the project's one deliberate, documented
// exception to "never use deprecated endpoints" (see
// .github/copilot-instructions.md).
func assetsUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:      "update",
		Usage:     "⚠ DEPRECATED upstream: update one or more assets (PUT /assets/{id})",
		ArgsUsage: "[ASSET_ID ...]",
		Description: "Updates fields on one or more assets, one request per ID (continues on per-ID error). " +
			"updateAsset is deprecated upstream with no working replacement; kept only because it is the sole " +
			"way to set an asset's capture date (used by client-workflow fix-album-dates).",
		Flags: []cli.Flag{
			idsFileFlag(),
			&cli.StringFlag{
				Name:  "date-time-original",
				Usage: "set the asset's original date and time",
			},
			&cli.StringFlag{
				Name:  "description",
				Usage: "set the asset description",
			},
			&cli.StringFlag{
				Name:  "is-favorite",
				Usage: "mark as favorite: true or false",
			},
			&cli.FloatFlag{
				Name:  "latitude",
				Usage: "set the latitude coordinate",
			},
			&cli.FloatFlag{
				Name:  "longitude",
				Usage: "set the longitude coordinate",
			},
			&cli.StringFlag{
				Name:  "live-photo-video-id",
				Usage: "set the live photo video asset `UUID`",
			},
			&cli.IntFlag{
				Name:  "rating",
				Usage: "set the rating: 1-5 (starred) or -1 (rejected)",
			},
			&cli.StringFlag{
				Name:  "visibility",
				Usage: "set visibility: archive, hidden, locked, or timeline",
			},
		},
		Action: assetsUpdate,
	}
}

func assetsUpdate(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}

	body, err := buildUpdateAssetDto(cmd)
	if err != nil {
		return err
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	failures := 0
	for _, id := range ids {
		resp, err := c.API.UpdateAssetWithResponse(ctx, id, *body)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: asset %s: %v\n", id, err)
			failures++
			continue
		}
		fmt.Printf("%s: updated\n", id)
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d assets failed", failures, len(ids))
	}
	return nil
}

// buildUpdateAssetDto maps CLI flags to the API request body. Every field is
// optional (a nil field leaves that asset field unchanged server-side).
func buildUpdateAssetDto(cmd *cli.Command) (*immichapi.UpdateAssetDto, error) {
	body := &immichapi.UpdateAssetDto{}

	if v := cmd.String("date-time-original"); v != "" {
		body.DateTimeOriginal = &v
	}
	if v := cmd.String("description"); v != "" {
		body.Description = &v
	}
	if v := cmd.String("live-photo-video-id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("invalid --live-photo-video-id %q: %w", v, err)
		}
		uid := openapi_types.UUID(id)
		body.LivePhotoVideoId = &uid
	}
	if v := cmd.String("visibility"); v != "" {
		vis := immichapi.AssetVisibility(v)
		body.Visibility = &vis
	}
	if err := setBoolFlag(cmd, "is-favorite", func(b bool) { body.IsFavorite = &b }); err != nil {
		return nil, err
	}
	if cmd.IsSet("latitude") {
		lat := float32(cmd.Float("latitude"))
		body.Latitude = &lat
	}
	if cmd.IsSet("longitude") {
		lon := float32(cmd.Float("longitude"))
		body.Longitude = &lon
	}
	if cmd.IsSet("rating") {
		rating := int(cmd.Int("rating"))
		body.Rating = &rating
	}

	return body, nil
}
