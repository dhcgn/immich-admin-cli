package workflows

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// defaultHEICGridTileSize is libheif's default HEIC/HEIF grid tile size in
// pixels. Confirmed via ffprobe against real assets in production libraries:
// every sampled HEIC (camera-native and converted) tiles its grid image at
// 512x512.
const defaultHEICGridTileSize = 512

// FindHEICTileDefectOptions controls the find-heic-tile-defect workflow.
type FindHEICTileDefectOptions struct {
	// PageSize is the number of assets to request per page (max 1000).
	PageSize int
	// TileSize is the assumed HEIF grid tile size in pixels. Defaults to 512
	// (defaultHEICGridTileSize) when <= 0.
	TileSize int
	// OriginalFileName, if non-empty, pre-filters the search to assets whose
	// original file name matches (substring, same as the API's behaviour).
	OriginalFileName string
	// AlbumIDs, if non-empty, restricts the search to assets in these albums
	// (MetadataSearchDto.albumIds).
	AlbumIDs []openapi_types.UUID
}

// AssetHEICTileDefect holds the minimal information about a HEIC/HEIF asset
// whose pixel dimensions are not an exact multiple of the grid tile size.
type AssetHEICTileDefect struct {
	ID               string
	OriginalFileName string
	OriginalPath     string
	Width            int
	Height           int
}

// isHEICFileName reports whether name has a HEIC/HEIF file extension.
func isHEICFileName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".heic") || strings.HasSuffix(lower, ".heif")
}

// hasHEICTileDefect reports whether an asset with the given width/height
// would need a non-uniform (cropped) last row/column of tiles when encoded as
// a HEIF grid at tileSize. A HEIC/HEIF file built that way is exactly the
// defect observed to make Immich (libvips/libheif) render a garbled,
// low-detail preview/thumbnail instead of the real image — confirmed by
// downloading and independently decoding several affected assets from a real
// library: the original bytes decode perfectly (e.g. via ImageMagick), only
// Immich's generated preview/thumbnail is wrong. Camera-native HEIC/HEIF
// tends to avoid this (sensor output sizes are often exact multiples of the
// tile size); HEIC produced by desktop conversion tools (observed: Zoner
// Photo Studio X, DxO PhotoLab) reliably hits it because they tile at a fixed
// default size regardless of the source image's dimensions.
//
// This is a structural heuristic, not a guaranteed defect: it assumes the
// standard 512px tile size and cannot see how a given file was actually
// encoded. Treat matches as candidates for manual/visual confirmation, not
// for automated deletion.
func hasHEICTileDefect(width, height, tileSize int) bool {
	if tileSize <= 0 {
		tileSize = defaultHEICGridTileSize
	}
	return width%tileSize != 0 || height%tileSize != 0
}

// FindAssetsWithHEICTileDefect pages through all IMAGE assets matching the
// optional pre-filters and returns the HEIC/HEIF ones whose dimensions are not
// an exact multiple of the grid tile size (see hasHEICTileDefect).
//
// The search is done via POST /search/metadata with automatic pagination, so
// no per-asset download or info call is needed: width/height/file name are
// all present on the search result.
func FindAssetsWithHEICTileDefect(
	ctx context.Context,
	c *client.Client,
	opts FindHEICTileDefectOptions,
) ([]AssetHEICTileDefect, error) {
	if opts.PageSize <= 0 || opts.PageSize > 1000 {
		opts.PageSize = 250
	}
	if opts.TileSize <= 0 {
		opts.TileSize = defaultHEICGridTileSize
	}

	imageType := immichapi.IMAGE
	body := immichapi.MetadataSearchDto{
		Size: &opts.PageSize,
		Type: &imageType,
	}
	if opts.OriginalFileName != "" {
		body.OriginalFileName = &opts.OriginalFileName
	}
	if len(opts.AlbumIDs) > 0 {
		body.AlbumIds = &opts.AlbumIDs
	}

	var results []AssetHEICTileDefect
	page := 1
	scanned := 0

	for {
		body.Page = &page

		resp, err := c.API.SearchAssetsWithResponse(ctx, &immichapi.SearchAssetsParams{}, body)
		if err != nil {
			return nil, fmt.Errorf("calling POST /search/metadata (page %d): %w", page, err)
		}
		if err := client.Check(resp, http.StatusOK); err != nil {
			return nil, fmt.Errorf("POST /search/metadata (page %d): %w", page, err)
		}

		assets := resp.JSON200.Assets

		for i := range assets.Items {
			a := &assets.Items[i]
			scanned++
			if !isHEICFileName(a.OriginalFileName) || a.Width == nil || a.Height == nil {
				continue
			}
			if hasHEICTileDefect(*a.Width, *a.Height, opts.TileSize) {
				results = append(results, AssetHEICTileDefect{
					ID:               a.Id.String(),
					OriginalFileName: a.OriginalFileName,
					OriginalPath:     a.OriginalPath,
					Width:            *a.Width,
					Height:           *a.Height,
				})
			}
		}

		fmt.Fprintf(
			os.Stderr,
			"\rScanned %d assets, found %d HEIC tile-defect candidate(s)...",
			scanned, len(results),
		)

		if assets.NextPage == nil || *assets.NextPage == "" {
			break
		}
		nextPage, err := strconv.Atoi(*assets.NextPage)
		if err != nil {
			break
		}
		page = nextPage
	}

	// Clear the progress line.
	fmt.Fprintln(os.Stderr)

	return results, nil
}

// ResolveOrCreateTag ensures a tag with the given full-path value (e.g.
// "immich-admin-cli/corrupt-heic") exists — creating it, and any missing
// parent tags implied by "/" in the path, via PUT /tags — and returns its ID.
func ResolveOrCreateTag(ctx context.Context, c *client.Client, value string) (openapi_types.UUID, error) {
	resp, err := c.API.UpsertTagsWithResponse(ctx, immichapi.TagUpsertDto{Tags: []string{value}})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return openapi_types.UUID{}, fmt.Errorf("upserting tag %q: %w", value, err)
	}
	for _, t := range *resp.JSON200 {
		if t.Value == value {
			return t.Id, nil
		}
	}
	return openapi_types.UUID{}, fmt.Errorf("upserting tag %q: server response did not include it", value)
}

// TagAssets assigns tagID to every asset in assetIDs in a single request
// (PUT /tags/assets). It is a no-op when assetIDs is empty.
func TagAssets(ctx context.Context, c *client.Client, assetIDs []openapi_types.UUID, tagID openapi_types.UUID) error {
	if len(assetIDs) == 0 {
		return nil
	}
	resp, err := c.API.BulkTagAssetsWithResponse(ctx, immichapi.TagBulkAssetsDto{
		AssetIds: assetIDs,
		TagIds:   []openapi_types.UUID{tagID},
	})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("tagging %d asset(s): %w", len(assetIDs), err)
	}
	return nil
}
