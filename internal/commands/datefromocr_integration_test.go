//go:build integration

// Staging roundtrip for `assets ocr` and the date-from-ocr write path
// (workflows.SetAssetsYear): uploads a throwaway PNG, reads its (empty) OCR,
// sets its year, verifies date and tag, and force-deletes it via the
// uploadStagingAsset cleanup. Run with:
//
//	go test -tags integration ./internal/commands/ -run DateFromOCR
package commands

import (
	"context"
	"net/http"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

func TestDateFromOCRRoundtripIntegration(t *testing.T) {
	c := stagingTestClient(t)
	ctx := context.Background()
	id := uploadStagingAsset(t, ctx, c)

	if _, err := workflows.FetchAssetOCR(ctx, c, id); err != nil {
		t.Fatalf("FetchAssetOCR: %v", err)
	}

	assets, err := workflows.FetchAssets(ctx, c, []openapi_types.UUID{id})
	if err != nil || len(assets) != 1 {
		t.Fatalf("FetchAssets: %v (%d)", err, len(assets))
	}
	if err := workflows.SetAssetsYear(ctx, c, assets, 1986, workflows.DateFromOCRTag, false); err != nil {
		t.Fatalf("SetAssetsYear: %v", err)
	}

	// The date may land via a server-side sidecar/metadata job, so poll briefly.
	var info *immichapi.AssetResponseDto
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := c.API.GetAssetInfoWithResponse(ctx, id, nil)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			t.Fatalf("GetAssetInfo: %v", err)
		}
		info = resp.JSON200
		if info.LocalDateTime.Year() == 1986 || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	// The naive timestamp is shifted by the server/asset time zone (hours
	// only; staging is UTC+x, so the date holds); mid-year means the year
	// never changes in any zone.
	if got := info.LocalDateTime.Format("2006-01-02"); got != "1986-06-01" {
		t.Errorf("LocalDateTime date = %s, want 1986-06-01", got)
	}

	tagged := false
	if info.Tags != nil {
		for _, tg := range *info.Tags {
			tagged = tagged || tg.Value == workflows.DateFromOCRTag
		}
	}
	if !tagged {
		t.Errorf("asset not tagged %q", workflows.DateFromOCRTag)
	}
}

// Read-only: the library-wide pre-filter (one OCR search per year) must run
// through without a server error; staging may simply have no OCR hits.
func TestDateFromOCRFindAllIntegration(t *testing.T) {
	c := stagingTestClient(t)
	assets, err := workflows.FindAssetsWithOCRYears(context.Background(), c, time.Now().Year())
	if err != nil {
		t.Fatalf("FindAssetsWithOCRYears: %v", err)
	}
	t.Logf("%d candidate asset(s)", len(assets))
}
