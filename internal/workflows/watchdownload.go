package workflows

import (
	"context"
	"fmt"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// SyncSource identifies one download mirror source: an album or a tag.
type SyncSource struct {
	Kind string
	ID   string
	Name string
}

// PlanSync is the generic read-only sync planner shared by album and tag
// sources. fetch lists the source's current (already filtered) assets.
func PlanSync(ctx context.Context, c *client.Client, source SyncSource, targetDir string, opts DownloadAlbumOptions, fetch func(ctx context.Context) ([]immichapi.AssetResponseDto, error)) ([]immichapi.AssetResponseDto, SyncPlan, Manifest, error) {
	manifest, existed, err := LoadManifest(targetDir)
	if err != nil {
		return nil, SyncPlan{}, Manifest{}, err
	}
	if existed {
		kind, id, name := manifest.normalizedSource()
		if kind != "" && (kind != source.Kind || id != source.ID) {
			return nil, SyncPlan{}, Manifest{}, fmt.Errorf("manifest in %q tracks %s %q (%s), not %s %q (%s) — use a different --target-dir", targetDir, kind, name, id, source.Kind, source.Name, source.ID)
		}
		if err := checkManifestOptions(targetDir, manifest, opts); err != nil {
			return nil, SyncPlan{}, Manifest{}, err
		}
	}

	assets, err := fetch(ctx)
	if err != nil {
		return nil, SyncPlan{}, Manifest{}, fmt.Errorf("fetching %s assets: %w", source.Kind, err)
	}
	return assets, ComputeSyncPlan(assets, manifest), manifest, nil
}

// PlanTagSync plans a tag-source sync (read-only).
func PlanTagSync(ctx context.Context, c *client.Client, tag immichapi.TagResponseDto, targetDir string, opts DownloadAlbumOptions) ([]immichapi.AssetResponseDto, SyncPlan, Manifest, error) {
	return PlanSync(ctx, c, SyncSource{Kind: SourceKindTag, ID: tag.Id.String(), Name: tag.Value}, targetDir, opts, func(ctx context.Context) ([]immichapi.AssetResponseDto, error) {
		return FetchFilteredTagAssets(ctx, c, tag.Id, opts.IgnoreVideos)
	})
}

// ResolveTagByValue finds exactly one tag by full-path value.
func ResolveTagByValue(ctx context.Context, c *client.Client, value string) (immichapi.TagResponseDto, error) {
	resp, err := c.API.GetAllTagsWithResponse(ctx)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return immichapi.TagResponseDto{}, fmt.Errorf("listing tags: %w", err)
	}
	var matches []immichapi.TagResponseDto
	if resp.JSON200 != nil {
		for _, t := range *resp.JSON200 {
			if t.Value == value {
				matches = append(matches, t)
			}
		}
	}
	if len(matches) == 0 {
		return immichapi.TagResponseDto{}, fmt.Errorf("no tag with value %q found", value)
	}
	if len(matches) > 1 {
		ids := make([]string, len(matches))
		for i, t := range matches {
			ids[i] = t.Id.String()
		}
		return immichapi.TagResponseDto{}, fmt.Errorf("%d tags have value %q — use --tag-id to disambiguate", len(matches), value)
	}
	return matches[0], nil
}

// FetchTagAssets returns every asset carrying tagID via tag-scoped metadata
// search (POST /search/metadata, MetadataSearchDto.TagIds), following the
// result cursor until exhausted.
func FetchTagAssets(ctx context.Context, c *client.Client, tagID openapi_types.UUID) ([]immichapi.AssetResponseDto, error) {
	var assets []immichapi.AssetResponseDto
	var pager SearchPager
	size := 250
	for {
		body := immichapi.MetadataSearchDto{
			TagIds: &[]openapi_types.UUID{tagID},
			Size:   &size,
		}
		pager.Apply(&body)

		resp, err := c.API.SearchAssetsWithResponse(ctx, &immichapi.SearchAssetsParams{}, body)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			return nil, fmt.Errorf("searching tag assets: %w", err)
		}
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("searching tag assets: response had no body")
		}
		assets = append(assets, resp.JSON200.Assets.Items...)
		if !pager.Next(resp.JSON200.Assets) {
			return assets, nil
		}
	}
}

// FetchFilteredTagAssets fetches every asset with tagID and, if
// ignoreVideos, drops VIDEO assets.
func FetchFilteredTagAssets(ctx context.Context, c *client.Client, tagID openapi_types.UUID, ignoreVideos bool) ([]immichapi.AssetResponseDto, error) {
	assets, err := FetchTagAssets(ctx, c, tagID)
	if err != nil {
		return nil, err
	}
	if ignoreVideos {
		assets = FilterOutVideos(assets)
	}
	return assets, nil
}

// WatchDownloadOptions controls one watch-download scan / loop.
type WatchDownloadOptions struct {
	DownloadAlbumOptions
	TargetDir string
	Source    SyncSource
	TagID     *openapi_types.UUID
	AlbumID   *openapi_types.UUID
	Interval  int64 // seconds; <=0 means run once (per user decision: treat as once)
	Once      bool
	DryRun    bool
	Quiet     bool
	JSON      bool
}

// RunWatchDownloadOnce runs a single sync scan from source into targetDir.
func RunWatchDownloadOnce(ctx context.Context, c *client.Client, opts WatchDownloadOptions) (SyncPlan, error) {
	dopts := opts.DownloadAlbumOptions
	dopts.DryRun = opts.DryRun
	dopts.Quiet = opts.Quiet || opts.JSON

	var assets []immichapi.AssetResponseDto
	var plan SyncPlan
	var manifest Manifest
	var err error
	switch opts.Source.Kind {
	case SourceKindTag:
		var tagID openapi_types.UUID
		if opts.TagID != nil {
			tagID = *opts.TagID
		} else {
			var tag immichapi.TagResponseDto
			tag, err = ResolveTagByValue(ctx, c, opts.Source.Name)
			if err != nil {
				return SyncPlan{}, err
			}
			tagID = tag.Id
			opts.Source.ID = tag.Id.String()
		}
		assets, plan, manifest, err = PlanSync(ctx, c, opts.Source, opts.TargetDir, dopts, func(ctx context.Context) ([]immichapi.AssetResponseDto, error) {
			return FetchFilteredTagAssets(ctx, c, tagID, dopts.IgnoreVideos)
		})
	default:
		var albumID openapi_types.UUID
		if opts.AlbumID != nil {
			albumID = *opts.AlbumID
		}
		assets, plan, manifest, err = PlanSync(ctx, c, opts.Source, opts.TargetDir, dopts, func(ctx context.Context) ([]immichapi.AssetResponseDto, error) {
			return FetchFilteredAlbumAssets(ctx, c, albumID, dopts.IgnoreVideos)
		})
	}
	if err != nil {
		return SyncPlan{}, err
	}
	if opts.DryRun {
		return plan, nil
	}
	if err := ApplySync(ctx, c, opts.Source, opts.TargetDir, assets, plan, manifest, dopts); err != nil {
		return plan, err
	}
	return plan, nil
}
