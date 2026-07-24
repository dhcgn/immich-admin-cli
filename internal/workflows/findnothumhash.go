package workflows

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// FindNoThumbhashOptions controls the find-assets-with-no-thumbhash workflow.
type FindNoThumbhashOptions struct {
	// PageSize is the number of assets to request per page (max 1000).
	PageSize int
	// OriginalFileName, if non-empty, pre-filters the search to assets whose
	// original file name matches (substring, same as the API's behaviour).
	OriginalFileName string
	// Type, if non-empty, pre-filters by asset type (IMAGE, VIDEO, …).
	Type string
	// AlbumIDs, if non-empty, restricts the search to assets in these albums
	// (MetadataSearchDto.albumIds).
	AlbumIDs []openapi_types.UUID
}

// AssetNoThumbhash holds the minimal information about an asset that has no
// thumbhash.
type AssetNoThumbhash struct {
	ID               string
	OriginalFileName string
	Type             string
	OriginalPath     string
}

// FindAssetsWithNoThumbhash pages through all assets matching the optional
// pre-filters and returns those whose thumbhash field is null or empty.
//
// The search is done via POST /search/metadata with automatic pagination,
// so no per-asset info call is needed.
func FindAssetsWithNoThumbhash(
	ctx context.Context,
	c *client.Client,
	opts FindNoThumbhashOptions,
) ([]AssetNoThumbhash, error) {
	if opts.PageSize <= 0 || opts.PageSize > 1000 {
		opts.PageSize = 250
	}

	body := immichapi.MetadataSearchDto{
		Size: &opts.PageSize,
	}
	if opts.OriginalFileName != "" {
		body.OriginalFileName = &opts.OriginalFileName
	}
	if opts.Type != "" {
		t := immichapi.AssetTypeEnum(opts.Type)
		body.Type = &t
	}
	if len(opts.AlbumIDs) > 0 {
		body.AlbumIds = &opts.AlbumIDs
	}

	var results []AssetNoThumbhash
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
			if a.Thumbhash == nil || *a.Thumbhash == "" {
				results = append(results, AssetNoThumbhash{
					ID:               a.Id.String(),
					OriginalFileName: a.OriginalFileName,
					Type:             string(a.Type),
					OriginalPath:     a.OriginalPath,
				})
			}
		}

		fmt.Fprintf(
			os.Stderr,
			"\rScanned %d assets, found %d without thumbhash...",
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

// GetAlbumSummary fetches an album by ID (GET /albums/{id}, getAlbumInfo) and
// returns its name and asset count, validating that the album exists. It is
// used to confirm an --album-id and give friendly output before scanning the
// album's assets. (This spec's AlbumResponseDto carries no asset list, so the
// assets themselves are fetched via the metadata-search finder, filtered by
// albumIds.)
func GetAlbumSummary(ctx context.Context, c *client.Client, id openapi_types.UUID) (name string, count int, err error) {
	resp, err := c.API.GetAlbumInfoWithResponse(ctx, id, &immichapi.GetAlbumInfoParams{})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return "", 0, fmt.Errorf("fetching album %s: %w", id, err)
	}
	return resp.JSON200.AlbumName, resp.JSON200.AssetCount, nil
}
