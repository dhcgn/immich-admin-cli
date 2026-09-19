//go:build integration

// Staging integration tests for issue #38 (`albums add-assets`,
// `remove-assets`, `create`, `delete`, and the `client-workflow merge-album`
// workflow). They reuse stagingTestClient/writeRandomPNG/stagingConfigPath
// and skip when config.staging.yaml is absent. Everything created here is
// self-cleaning: albums get a t.Cleanup DeleteAlbum safety net and uploaded
// assets a force-delete safety net, so a mid-test failure never leaks.
//
// Run explicitly with:
//
//	go test -tags integration ./internal/commands/ -run 'AlbumAssetsManage|AlbumMerge'
package commands

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// createStagingAlbum creates a throwaway album and registers a t.Cleanup
// safety net that deletes it even if the test fails halfway.
func createStagingAlbum(t *testing.T, ctx context.Context, c *client.Client, name string) openapi_types.UUID {
	t.Helper()
	resp, err := c.API.CreateAlbumWithResponse(ctx, immichapi.CreateAlbumDto{AlbumName: name})
	if err == nil {
		err = client.Check(resp, http.StatusCreated)
	}
	if err != nil {
		t.Fatalf("CreateAlbum(%q): %v", name, err)
	}
	if resp.JSON201 == nil {
		t.Fatalf("CreateAlbum(%q): response had no body", name)
	}
	id := resp.JSON201.Id
	t.Cleanup(func() {
		del, err := c.API.DeleteAlbumWithResponse(ctx, id)
		if err == nil {
			err = client.Check(del, http.StatusNoContent)
		}
		if err != nil {
			t.Logf("cleanup: deleting album %s: %v", id, err)
		}
	})
	return id
}

// uploadStagingAsset uploads a generated 1x1 PNG and registers a t.Cleanup
// force-delete safety net so staging never keeps a leftover asset.
func uploadStagingAsset(t *testing.T, ctx context.Context, c *client.Client) openapi_types.UUID {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("album-test-%d.png", time.Now().UnixNano()))
	writeRandomPNG(t, path)
	id, err := workflows.UploadAssetFile(ctx, c, path, workflows.UploadOptions{})
	if err != nil {
		t.Fatalf("UploadAssetFile: %v", err)
	}
	t.Cleanup(func() {
		force := true
		del, err := c.API.DeleteAssetsWithResponse(ctx, immichapi.AssetBulkDeleteDto{Ids: []openapi_types.UUID{id}, Force: &force})
		if err == nil {
			err = client.Check(del, http.StatusNoContent)
		}
		if err != nil {
			t.Logf("cleanup: deleting asset %s: %v", id, err)
		}
	})
	return id
}

// albumAssetCount returns the album's current AssetCount (GET /albums/{id}).
func albumAssetCount(t *testing.T, ctx context.Context, c *client.Client, id openapi_types.UUID) int {
	t.Helper()
	resp, err := c.API.GetAlbumInfoWithResponse(ctx, id, &immichapi.GetAlbumInfoParams{})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("GetAlbumInfo(%s): %v", id, err)
	}
	return resp.JSON200.AssetCount
}

// waitAlbumGone polls GET /albums/{id} until it stops returning 200: album
// deletion finishes in a background job server-side, so the 204 does not
// mean the album is immediately unresolvable.
func waitAlbumGone(t *testing.T, ctx context.Context, c *client.Client, id openapi_types.UUID) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := c.API.GetAlbumInfoWithResponse(ctx, id, &immichapi.GetAlbumInfoParams{})
		if err != nil {
			t.Fatalf("GetAlbumInfo after delete: %v", err)
		}
		if resp.StatusCode() != http.StatusOK {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("GetAlbumInfo(%s) still returns 200 30s after delete", id)
		}
		time.Sleep(2 * time.Second)
	}
}

func TestAlbumAssetsManageRoundtripIntegration(t *testing.T) {
	c := stagingTestClient(t)
	ctx := context.Background()
	base := fmt.Sprintf("album-manage-%d", time.Now().UnixNano())

	asset1 := uploadStagingAsset(t, ctx, c)
	asset2 := uploadStagingAsset(t, ctx, c)
	album := createStagingAlbum(t, ctx, c, base)

	// 1. Add both assets (PUT /albums/{id}/assets): every entry must succeed.
	add, err := c.API.AddAssetsToAlbumWithResponse(ctx, album, immichapi.BulkIdsDto{Ids: []openapi_types.UUID{asset1, asset2}})
	if err == nil {
		err = client.Check(add, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("AddAssetsToAlbum: %v", err)
	}
	for _, r := range *add.JSON200 {
		if !r.Success {
			t.Errorf("AddAssetsToAlbum(%s): success=false (%s)", r.Id, bulkIDError(r))
		}
	}
	if got := albumAssetCount(t, ctx, c, album); got != 2 {
		t.Fatalf("AssetCount = %d, want 2", got)
	}

	// 2. Re-adding reports duplicates instead of failing.
	dup, err := c.API.AddAssetsToAlbumWithResponse(ctx, album, immichapi.BulkIdsDto{Ids: []openapi_types.UUID{asset1}})
	if err == nil {
		err = client.Check(dup, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("AddAssetsToAlbum(duplicate): %v", err)
	}
	if got := tallyBulkResults(*dup.JSON200); got.alreadyPresent != 1 {
		t.Errorf("duplicate add tally = %+v, want 1 already-present", got)
	}

	// 3. Remove one asset (DELETE /albums/{id}/assets).
	rem, err := c.API.RemoveAssetFromAlbumWithResponse(ctx, album, immichapi.BulkIdsDto{Ids: []openapi_types.UUID{asset1}})
	if err == nil {
		err = client.Check(rem, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("RemoveAssetFromAlbum: %v", err)
	}
	for _, r := range *rem.JSON200 {
		if !r.Success {
			t.Errorf("RemoveAssetFromAlbum(%s): success=false (%s)", r.Id, bulkIDError(r))
		}
	}
	if got := albumAssetCount(t, ctx, c, album); got != 1 {
		t.Fatalf("AssetCount = %d, want 1", got)
	}

	// 4. Delete the album (DELETE /albums/{id}) and wait for it to vanish.
	del, err := c.API.DeleteAlbumWithResponse(ctx, album)
	if err == nil {
		err = client.Check(del, http.StatusNoContent)
	}
	if err != nil {
		t.Fatalf("DeleteAlbum(%s): %v", album, err)
	}
	waitAlbumGone(t, ctx, c, album)
	t.Logf("managed album %s: added 2, removed 1, deleted", album)
}

func TestAlbumMergeRoundtripIntegration(t *testing.T) {
	c := stagingTestClient(t)
	ctx := context.Background()
	base := fmt.Sprintf("album-merge-%d", time.Now().UnixNano())

	asset := uploadStagingAsset(t, ctx, c)
	source := createStagingAlbum(t, ctx, c, base+"-source")
	target := createStagingAlbum(t, ctx, c, base+"-target")

	add, err := c.API.AddAssetsToAlbumWithResponse(ctx, source, immichapi.BulkIdsDto{Ids: []openapi_types.UUID{asset}})
	if err == nil {
		err = client.Check(add, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("AddAssetsToAlbum(source): %v", err)
	}

	if err := workflows.MergeAlbums(ctx, c, workflows.MergeAlbumOptions{
		From:              source,
		Into:              target,
		DeleteEmptySource: true,
	}); err != nil {
		t.Fatalf("MergeAlbums: %v", err)
	}

	if got := albumAssetCount(t, ctx, c, target); got != 1 {
		t.Errorf("target AssetCount = %d, want 1", got)
	}
	waitAlbumGone(t, ctx, c, source)
	t.Logf("merged album %s into %s and deleted the source", source, target)
}
