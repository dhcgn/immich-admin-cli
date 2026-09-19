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
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// albumsAddAssetsCommand exposes PUT /albums/{id}/assets (addAssetsToAlbum).
func albumsAddAssetsCommand() *cli.Command {
	return &cli.Command{
		Name:      "add-assets",
		Usage:     "Add assets to an album (PUT /albums/{id}/assets)",
		ArgsUsage: "ALBUM_ID [ASSET_ID ...]",
		Flags: []cli.Flag{
			idsFileFlag(),
			&cli.BoolFlag{Name: "dry-run", Usage: "print the assets that would be added without changing anything"},
			&cli.BoolFlag{Name: "yes", Usage: "skip the confirmation prompt before adding assets"},
		},
		Action: albumsAddAssets,
	}
}

// albumsRemoveAssetsCommand exposes DELETE /albums/{id}/assets
// (removeAssetFromAlbum).
func albumsRemoveAssetsCommand() *cli.Command {
	return &cli.Command{
		Name:      "remove-assets",
		Usage:     "Remove assets from an album (DELETE /albums/{id}/assets)",
		ArgsUsage: "ALBUM_ID [ASSET_ID ...]",
		Flags: []cli.Flag{
			idsFileFlag(),
			&cli.BoolFlag{Name: "dry-run", Usage: "print the assets that would be removed without changing anything"},
			&cli.BoolFlag{Name: "yes", Usage: "skip the confirmation prompt before removing assets"},
		},
		Action: albumsRemoveAssets,
	}
}

func albumsAddAssets(ctx context.Context, cmd *cli.Command) error {
	return albumBulkAssets(ctx, cmd, "add", "added")
}

func albumsRemoveAssets(ctx context.Context, cmd *cli.Command) error {
	return albumBulkAssets(ctx, cmd, "remove", "removed")
}

// albumBulkAssets runs `albums add-assets` (verb "add") or `albums
// remove-assets` (verb "remove") against one album: ALBUM_ID is the first
// positional argument, asset IDs come from the remaining positionals and/or
// --ids-file. Per-ID results come back as BulkIdResponseDto and are tallied
// (added / already-present / not-found); anything else failing is a hard
// failure reflected in the exit code.
func albumBulkAssets(ctx context.Context, cmd *cli.Command, verb, past string) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return fmt.Errorf("no album ID given: pass ALBUM_ID as the first argument")
	}
	albumID, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid ALBUM_ID %q: %w", args[0], err)
	}
	// Asset IDs come from the remaining positionals and/or --ids-file.
	// collectIDs reads cmd.Args() directly, so scope it to the tail.
	assetIDs, err := collectIDsFrom(cmd, args[1:])
	if err != nil {
		return err
	}

	if cmd.Bool("dry-run") {
		for _, id := range assetIDs {
			fmt.Printf("[dry-run] would %s asset %s to album %s\n", verb, id, albumID)
		}
		fmt.Printf("[dry-run] %d asset(s) would be %s\n", len(assetIDs), past)
		return nil
	}

	if !cmd.Bool("yes") {
		fmt.Printf("This will %s %d asset(s) to album %s.\n", verb, len(assetIDs), albumID)
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

	uid := openapi_types.UUID(albumID)
	return workflows.RunBatch(chunkUUIDs(assetIDs, 500),
		func(chunk []openapi_types.UUID) string { return fmt.Sprintf("%d asset(s)", len(chunk)) },
		func(chunk []openapi_types.UUID) error {
			var results []immichapi.BulkIdResponseDto
			if verb == "add" {
				resp, err := c.API.AddAssetsToAlbumWithResponse(ctx, uid, immichapi.BulkIdsDto{Ids: chunk})
				if err == nil {
					err = client.Check(resp, http.StatusOK)
				}
				if err != nil {
					return fmt.Errorf("%sing %d asset(s): %w", verb, len(chunk), err)
				}
				if resp.JSON200 != nil {
					results = *resp.JSON200
				}
			} else {
				resp, err := c.API.RemoveAssetFromAlbumWithResponse(ctx, uid, immichapi.BulkIdsDto{Ids: chunk})
				if err == nil {
					err = client.Check(resp, http.StatusOK)
				}
				if err != nil {
					return fmt.Errorf("%sing %d asset(s): %w", verb, len(chunk), err)
				}
				if resp.JSON200 != nil {
					results = *resp.JSON200
				}
			}
			return reportAlbumBulkResults(past, results)
		},
	)
}

// bulkTally counts per-ID bulk outcomes: successes, already-present
// (duplicate) and not-found IDs are reported, not failed; every other
// failure is collected for the summary error.
type bulkTally struct {
	succeeded, alreadyPresent, notFound int
	failures                            []string
}

// tallyBulkResults sorts BulkIdResponseDto entries into a bulkTally. Pure
// (no I/O) so the reporting buckets are directly unit-testable.
func tallyBulkResults(results []immichapi.BulkIdResponseDto) bulkTally {
	var t bulkTally
	for _, r := range results {
		switch {
		case r.Success:
			t.succeeded++
		case r.Error != nil && *r.Error == immichapi.BulkIdErrorReasonDuplicate:
			t.alreadyPresent++
		case r.Error != nil && *r.Error == immichapi.BulkIdErrorReasonNotFound:
			t.notFound++
		default:
			t.failures = append(t.failures, fmt.Sprintf("%s: %s", r.Id, bulkIDError(r)))
		}
	}
	return t
}

// reportAlbumBulkResults prints per-ID lines for one bulk response and the
// totals line (e.g. "3 added, 1 already present, 1 not found"), returning a
// summary error when any ID hard-failed.
func reportAlbumBulkResults(past string, results []immichapi.BulkIdResponseDto) error {
	t := tallyBulkResults(results)
	for _, f := range t.failures {
		fmt.Fprintf(os.Stderr, "Error: asset %s\n", f)
	}
	fmt.Printf("%d %s, %d already present, %d not found\n", t.succeeded, past, t.alreadyPresent, t.notFound)
	if len(t.failures) > 0 {
		return fmt.Errorf("%d of %d assets failed", len(t.failures), len(results))
	}
	return nil
}

// albumsAssetsCommand lists every asset in one album. GET /albums/{id}
// carries no asset list, so this runs the album-scoped metadata search
// (POST /search/metadata) via workflows.FetchAlbumAssets.
func albumsAssetsCommand() *cli.Command {
	return &cli.Command{
		Name:  "assets",
		Usage: "List every asset in one album (POST /search/metadata)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "album-id", Usage: "album `UUID` (mutually exclusive with --album-name)"},
			&cli.StringFlag{Name: "album-name", Usage: "album name, exact match (mutually exclusive with --album-id)"},
			&cli.BoolFlag{Name: "json", Usage: "print the raw responses as a JSON array"},
			&cli.BoolFlag{
				Name:    "ids-only",
				Aliases: []string{"q"},
				Usage:   "print only asset IDs, one per line (useful for piping to other commands)",
			},
			&cli.BoolFlag{Name: "yes", Usage: "auto-accept a whitespace-variant album name match without prompting"},
		},
		Action: albumsAssets,
	}
}

func albumsAssets(ctx context.Context, cmd *cli.Command) error {
	albumIDStr := cmd.String("album-id")
	albumName := cmd.String("album-name")
	if err := validateAlbumIDNameFlags(albumIDStr, albumName); err != nil {
		return err
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	var album immichapi.AlbumResponseDto
	if albumIDStr != "" {
		id, err := uuid.Parse(albumIDStr)
		if err != nil {
			return fmt.Errorf("invalid --album-id %q: %w", albumIDStr, err)
		}
		uid := openapi_types.UUID(id)
		album, err = workflows.ResolveAlbum(ctx, c, &uid, "")
		if err != nil {
			return err
		}
	} else {
		album, err = resolveAlbumByName(ctx, c, os.Stdin, os.Stdout, albumName, cmd.Bool("yes"))
		if err != nil {
			return err
		}
	}

	assets, err := workflows.FetchAlbumAssets(ctx, c, album.Id)
	if err != nil {
		return err
	}
	if assets == nil {
		assets = []immichapi.AssetResponseDto{}
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(assets)
	}
	if cmd.Bool("ids-only") {
		for _, a := range assets {
			fmt.Println(a.Id)
		}
		return nil
	}
	for _, a := range assets {
		fmt.Printf("%s\t%s\t%s\n", a.Id, a.OriginalFileName, a.Type)
	}
	fmt.Printf("%d asset(s) in album %s %q\n", len(assets), album.Id, album.AlbumName)
	return nil
}
