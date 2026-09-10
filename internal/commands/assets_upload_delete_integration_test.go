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
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand"
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

// writeRandomPNG writes a valid 1x1 PNG with a random pixel to path and
// returns its bytes. Random content keeps every run unique so the server's
// checksum-based duplicate detection never fires across runs — only the
// test's own intentional re-upload of the same file may hit it. Hermetic,
// no dependency on .prod-test-data/.
func writeRandomPNG(t *testing.T, path string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{
		R: byte(rand.Intn(256)),
		G: byte(rand.Intn(256)),
		B: byte(rand.Intn(256)),
		A: 0xff,
	})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding fixture PNG: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return buf.Bytes()
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
	fixture := writeRandomPNG(t, path)

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
		// "Not found" just means step 4 already deleted it — the common case.
		if err != nil && !strings.Contains(err.Error(), "Not found") {
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
	sum := sha1.Sum(fixture)
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
