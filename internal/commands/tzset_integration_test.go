//go:build integration

// Staging roundtrip for client-workflow tz-audit/tz-set: uploads a JPEG whose
// EXIF DateTimeOriginal has no offset (→ timeZone null), finds it via the
// tz-audit search, sets Europe/Berlin, waits for Immich's sidecar-write →
// metadata re-extraction, and verifies the zone and the unchanged wall clock.
// Force-deletes the asset in t.Cleanup. Run with:
//
//	go test -tags integration ./internal/commands/ -run TZSet
package commands

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/config"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// writeJPEGWithNaiveDate writes a random 8x8 JPEG carrying a minimal EXIF
// APP1 segment (IFD0 → ExifIFD → DateTimeOriginal) without any offset tag.
func writeJPEGWithNaiveDate(t *testing.T, path, dateTimeOriginal string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{byte(rand.Intn(256)), byte(rand.Intn(256)), byte(rand.Intn(256)), 0xff})
	var j bytes.Buffer
	if err := jpeg.Encode(&j, img, nil); err != nil {
		t.Fatal(err)
	}
	var tiff bytes.Buffer
	w := func(v any) { _ = binary.Write(&tiff, binary.LittleEndian, v) }
	tiff.WriteString("II")
	w(uint16(42))
	w(uint32(8)) // IFD0 offset
	// IFD0: one entry, ExifIFD pointer (tag 0x8769, LONG) → offset 26.
	w(uint16(1))
	w(uint16(0x8769))
	w(uint16(4))
	w(uint32(1))
	w(uint32(26))
	w(uint32(0))
	// ExifIFD: one entry, DateTimeOriginal (0x9003, ASCII, 20 bytes) → offset 44.
	w(uint16(1))
	w(uint16(0x9003))
	w(uint16(2))
	w(uint32(20))
	w(uint32(44))
	w(uint32(0))
	tiff.WriteString(dateTimeOriginal + "\x00")
	seg := append([]byte("Exif\x00\x00"), tiff.Bytes()...)

	var out bytes.Buffer
	out.Write(j.Bytes()[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&out, binary.BigEndian, uint16(len(seg)+2))
	out.Write(seg)
	out.Write(j.Bytes()[2:])
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getAsset(t *testing.T, ctx context.Context, c *client.Client, id openapi_types.UUID) immichapi.AssetResponseDto {
	t.Helper()
	resp, err := c.API.GetAssetInfoWithResponse(ctx, id, nil)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("GetAssetInfo(%s): %v", id, err)
	}
	return *resp.JSON200
}

func TestTZSetRoundtripIntegration(t *testing.T) {
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

	name := "tzset-" + time.Now().Format("20060102150405") + ".jpg"
	path := filepath.Join(t.TempDir(), name)
	writeJPEGWithNaiveDate(t, path, "2020:07:15 12:00:00")

	id, err := workflows.UploadAssetFile(ctx, c, path, workflows.UploadOptions{})
	if err != nil {
		t.Fatalf("UploadAssetFile: %v", err)
	}
	t.Cleanup(func() {
		force := true
		if _, err := c.API.DeleteAssetsWithResponse(ctx, immichapi.AssetBulkDeleteDto{Ids: []openapi_types.UUID{id}, Force: &force}); err != nil {
			t.Logf("cleanup: deleting %s: %v", id, err)
		}
	})

	// 1. tz-audit path: the search must find the asset (once metadata
	// extraction ran) and classify it as "none".
	like := name
	var found *immichapi.AssetResponseDto
	deadline := time.Now().Add(60 * time.Second)
	for found == nil {
		as, err := workflows.SearchAssetsWithExif(ctx, c, immichapi.MetadataSearchDto{
			Filter: &immichapi.SearchFilter{OriginalFileName: &immichapi.StringPatternFilter{Like: &like}},
		}, nil)
		if err != nil {
			t.Fatalf("SearchAssetsWithExif: %v", err)
		}
		for i := range as {
			if as[i].Id == id && as[i].ExifInfo != nil && as[i].ExifInfo.DateTimeOriginal != nil {
				found = &as[i]
			}
		}
		if found == nil {
			if time.Now().After(deadline) {
				t.Fatal("uploaded asset not searchable with EXIF after 60s")
			}
			time.Sleep(2 * time.Second)
		}
	}
	if got := workflows.ClassifyTZ(*found, nil); got[0] != workflows.TZClassNone {
		t.Fatalf("fresh upload classified %v, want none (timeZone=%v)", got, found.ExifInfo.TimeZone)
	}
	wall := found.LocalDateTime

	// 2. tz-set path.
	berlin, err := workflows.LoadIANAZone("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	todo, skips := workflows.SplitTZSet([]immichapi.AssetResponseDto{getAsset(t, ctx, c, id)}, berlin)
	if len(todo) != 1 {
		t.Fatalf("SplitTZSet: todo=%d skips=%v", len(todo), skips)
	}
	if n, err := workflows.SetAssetsTimeZone(ctx, c, todo, berlin, false); err != nil || n != 1 {
		t.Fatalf("SetAssetsTimeZone = %d, %v", n, err)
	}

	// 3. After the server's sidecar roundtrip the zone is the fixed offset.
	deadline = time.Now().Add(60 * time.Second)
	for {
		a := getAsset(t, ctx, c, id)
		tz := ""
		if a.ExifInfo != nil && a.ExifInfo.TimeZone != nil {
			tz = *a.ExifInfo.TimeZone
		}
		if tz == "UTC+2" && a.FileCreatedAt.Equal(time.Date(2020, 7, 15, 10, 0, 0, 0, time.UTC)) {
			if !a.LocalDateTime.Equal(wall) {
				t.Errorf("localDateTime = %s, want unchanged %s", a.LocalDateTime, wall)
			}
			// 4. A second run must skip it: never overwrite an existing zone.
			if todo, _ := workflows.SplitTZSet([]immichapi.AssetResponseDto{a}, berlin); len(todo) != 0 {
				t.Error("second tz-set run would change an already zoned asset")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 60s: timeZone=%q fileCreatedAt=%s localDateTime=%s, want UTC+2 / 2020-07-15T10:00Z / %s", tz, a.FileCreatedAt, a.LocalDateTime, wall)
		}
		time.Sleep(2 * time.Second)
	}
}
