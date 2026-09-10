//go:build integration

// This file holds a staging integration test that talks to a real Immich
// server configured in config.staging.yaml. It is excluded from the normal
// test build (and CI) by the `integration` build tag; run it explicitly with:
//
//	go test -tags integration ./internal/commands/ -run UploadDelete
//
// Unlike the read-only prod tests in internal/workflows, this test mutates
// the staging server: it uploads a generated 1x1 PNG, verifies it, deletes
// it again, and force-deletes any leftover in t.Cleanup so a mid-test
// failure never leaks an asset. Only run it against a throwaway server.
package commands

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/config"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// stagingConfigPath points at the repo-root config.staging.yaml relative to
// this package. The file is gitignored (config*.yaml) because it holds the
// staging server URL and API key; the test skips when it is absent.
const stagingConfigPath = "../../config.staging.yaml"

// tinyPNG is a valid 1x1 transparent PNG — a hermetic upload fixture, no
// dependency on .prod-test-data/.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestAssetsUploadDeleteRoundtripIntegration(t *testing.T) {
	if _, err := os.Stat(stagingConfigPath); err != nil {
		t.Skipf("skipping: %s not found (%v)", filepath.Clean(stagingConfigPath), err)
	}

	cfg, err := config.Load(stagingConfigPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "staging-upload-test.png")
	if err := os.WriteFile(path, tinyPNG, 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	// Safety net: force-delete the upload even if the test fails halfway,
	// so staging never keeps a leftover asset.
	var uploaded openapi_types.UUID
	t.Cleanup(func() {
		if uploaded == (openapi_types.UUID{}) {
			return
		}
		force := true
		resp, err := c.API.DeleteAssetsWithResponse(ctx, immichapi.AssetBulkDeleteDto{Ids: []openapi_types.UUID{uploaded}, Force: &force})
		if err == nil {
			err = client.Check(resp, http.StatusNoContent)
		}
		if err != nil {
			t.Logf("cleanup: deleting %s: %v", uploaded, err)
		}
	})

	// 1. Upload — the same seam `assets upload` calls.
	id, err := workflows.UploadAssetFile(ctx, c, path, workflows.UploadOptions{})
	if err != nil {
		t.Fatalf("UploadAssetFile: %v", err)
	}
	uploaded = id
	t.Logf("uploaded %s as %s", path, id)

	// 2. Verify: the asset exists and its server-side checksum matches.
	info, err := c.API.GetAssetInfoWithResponse(ctx, id, nil)
	if err == nil {
		err = client.Check(info, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("GetAssetInfo(%s): %v", id, err)
	}
	sum := sha1.Sum(tinyPNG)
	if want := base64.StdEncoding.EncodeToString(sum[:]); info.JSON200.Checksum != want {
		t.Errorf("checksum = %q, want %q", info.JSON200.Checksum, want)
	}

	// 3. Re-uploading identical bytes must hit the duplicate (200) path.
	if _, err := workflows.UploadAssetFile(ctx, c, path, workflows.UploadOptions{}); err == nil {
		t.Error("re-uploading identical bytes: expected duplicate error, got nil")
	} else if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("re-uploading identical bytes: error = %v, want it to mention a duplicate", err)
	}

	// 4. Delete with force=true so staging stays clean (the CLI defaults to
	// trash; here full removal is the point).
	force := true
	del, err := c.API.DeleteAssetsWithResponse(ctx, immichapi.AssetBulkDeleteDto{Ids: []openapi_types.UUID{id}, Force: &force})
	if err == nil {
		err = client.Check(del, http.StatusNoContent)
	}
	if err != nil {
		t.Fatalf("DeleteAssets(%s): %v", id, err)
	}

	// 5. The asset must be gone. Deletion takes effect asynchronously
	// server-side (GetAssetInfo can still return 200 briefly after the
	// 204), so poll until it is trashed or unresolvable.
	deadline := time.Now().Add(30 * time.Second)
	for {
		gone, err := c.API.GetAssetInfoWithResponse(ctx, id, nil)
		if err != nil {
			t.Fatalf("GetAssetInfo after delete: %v", err)
		}
		if gone.StatusCode() != http.StatusOK || (gone.JSON200 != nil && gone.JSON200.IsTrashed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("GetAssetInfo(%s) still returns untrashed 200 30s after delete", id)
		}
		time.Sleep(2 * time.Second)
	}
}
