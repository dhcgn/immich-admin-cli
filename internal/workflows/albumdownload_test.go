package workflows

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func asset(id uuid.UUID, name string, typ immichapi.AssetTypeEnum, checksum string) immichapi.AssetResponseDto {
	return immichapi.AssetResponseDto{
		Id:               openapi_types.UUID(id),
		OriginalFileName: name,
		Type:             typ,
		Checksum:         checksum,
	}
}

func TestFilterOutVideos(t *testing.T) {
	img := asset(uuid.New(), "a.jpg", immichapi.IMAGE, "c1")
	vid := asset(uuid.New(), "b.mp4", immichapi.VIDEO, "c2")
	other := asset(uuid.New(), "c.bin", immichapi.OTHER, "c3")

	got := FilterOutVideos([]immichapi.AssetResponseDto{img, vid, other})

	want := []immichapi.AssetResponseDto{img, other}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FilterOutVideos() = %v, want %v", got, want)
	}
}

func TestAssignLocalNames(t *testing.T) {
	a1 := asset(uuid.New(), "IMG_1234.jpg", immichapi.IMAGE, "c1")
	a2 := asset(uuid.New(), "clip.mp4", immichapi.VIDEO, "c2")
	// Same base name, different case, as a1 above but distinct extension is
	// irrelevant here since AssignLocalNames strips extensions before
	// comparing.
	a3 := asset(uuid.New(), "img_1234.HEIC", immichapi.IMAGE, "c3")

	names := AssignLocalNames([]immichapi.AssetResponseDto{a1, a2, a3}, false)

	if len(names) != 3 {
		t.Fatalf("AssignLocalNames() returned %d entries, want 3", len(names))
	}
	// a2 has no collision: its base name is untouched.
	if got := names[a2.Id.String()]; got != "clip" {
		t.Errorf("a2 name = %q, want %q", got, "clip")
	}
	// a1 and a3 collide (case-insensitively) and must get distinct,
	// deterministic suffixed names rather than overwriting each other.
	n1, n3 := names[a1.Id.String()], names[a3.Id.String()]
	if n1 == n3 {
		t.Fatalf("colliding assets got identical names: %q", n1)
	}
	for _, n := range []string{n1, n3} {
		if len(n) <= len("img_1234") {
			t.Errorf("colliding name %q was not suffixed", n)
		}
	}

	// Re-running with the same (differently ordered) input must produce the
	// same mapping — the whole point of sorting by ID before assigning.
	reordered := []immichapi.AssetResponseDto{a3, a1, a2}
	names2 := AssignLocalNames(reordered, false)
	if !reflect.DeepEqual(names, names2) {
		t.Errorf("AssignLocalNames() not deterministic across input order: %v vs %v", names, names2)
	}
}

func TestAssignLocalNamesTimestampPrefix(t *testing.T) {
	when := time.Date(2025, 7, 4, 14, 4, 2, 0, time.UTC)
	a := asset(uuid.New(), "IMG_1234.jpg", immichapi.IMAGE, "c1")
	a.LocalDateTime = when

	names := AssignLocalNames([]immichapi.AssetResponseDto{a}, true)

	want := "2025-07-04_14_04_02_IMG_1234"
	if got := names[a.Id.String()]; got != want {
		t.Errorf("AssignLocalNames() with timestampPrefix = %q, want %q", got, want)
	}
}

func TestAssignLocalNamesTimestampPrefixAvoidsCollisionsBetweenDifferentTimes(t *testing.T) {
	a1 := asset(uuid.New(), "IMG_1234.jpg", immichapi.IMAGE, "c1")
	a1.LocalDateTime = time.Date(2025, 7, 4, 14, 4, 2, 0, time.UTC)
	a2 := asset(uuid.New(), "IMG_1234.jpg", immichapi.IMAGE, "c2")
	a2.LocalDateTime = time.Date(2025, 7, 5, 9, 0, 0, 0, time.UTC)

	names := AssignLocalNames([]immichapi.AssetResponseDto{a1, a2}, true)

	n1, n2 := names[a1.Id.String()], names[a2.Id.String()]
	if n1 == n2 {
		t.Fatalf("expected distinct names for different capture times, got %q for both", n1)
	}
	// Neither should carry a collision suffix: the timestamp prefix already
	// disambiguates them.
	for _, n := range []string{n1, n2} {
		if strings.Contains(n, a1.Id.String()[len(a1.Id.String())-8:]) || strings.Contains(n, a2.Id.String()[len(a2.Id.String())-8:]) {
			t.Errorf("name %q unexpectedly suffixed with an asset ID fragment", n)
		}
	}
}

func TestComputeSyncPlan(t *testing.T) {
	unchanged := asset(uuid.New(), "unchanged.jpg", immichapi.IMAGE, "same")
	changed := asset(uuid.New(), "changed.jpg", immichapi.IMAGE, "new-checksum")
	added := asset(uuid.New(), "added.jpg", immichapi.IMAGE, "c-added")
	removedID := uuid.New().String()

	manifest := Manifest{
		Assets: map[string]ManifestAsset{
			unchanged.Id.String(): {FileName: "unchanged.jpg", Checksum: "same"},
			changed.Id.String():   {FileName: "changed.jpg", Checksum: "old-checksum"},
			removedID:             {FileName: "gone.jpg", Checksum: "whatever"},
		},
	}

	plan := ComputeSyncPlan([]immichapi.AssetResponseDto{unchanged, changed, added}, manifest)

	if len(plan.Additions) != 1 || plan.Additions[0].Id.String() != added.Id.String() {
		t.Errorf("Additions = %v, want [added]", plan.Additions)
	}
	if len(plan.Updates) != 1 || plan.Updates[0].Id.String() != changed.Id.String() {
		t.Errorf("Updates = %v, want [changed]", plan.Updates)
	}
	if len(plan.Unchanged) != 1 || plan.Unchanged[0].Id.String() != unchanged.Id.String() {
		t.Errorf("Unchanged = %v, want [unchanged]", plan.Unchanged)
	}
	if len(plan.Removals) != 1 || plan.Removals[0].AssetID != removedID || plan.Removals[0].FileName != "gone.jpg" {
		t.Errorf("Removals = %v, want [{%s gone.jpg}]", plan.Removals, removedID)
	}
}

func TestComputeSyncPlanRemovalsSortedDeterministically(t *testing.T) {
	manifest := Manifest{
		Assets: map[string]ManifestAsset{
			"b-id": {FileName: "b.jpg"},
			"a-id": {FileName: "a.jpg"},
			"c-id": {FileName: "c.jpg"},
		},
	}

	plan := ComputeSyncPlan(nil, manifest)

	var got []string
	for _, r := range plan.Removals {
		got = append(got, r.AssetID)
	}
	want := []string{"a-id", "b-id", "c-id"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Removals order = %v, want %v (sorted)", got, want)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("Removals not sorted: %v", got)
	}
}

func TestManifestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	m := Manifest{
		AlbumID:   "album-1",
		AlbumName: "Vacation",
		Size:      immichapi.Thumbnail,
		Assets: map[string]ManifestAsset{
			"asset-1": {FileName: "a.webp", Checksum: "c1", Type: "IMAGE"},
		},
	}

	if err := SaveManifest(dir, m); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ManifestFileName)); err != nil {
		t.Fatalf("manifest file not written: %v", err)
	}

	got, existed, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if !existed {
		t.Fatalf("LoadManifest() existed = false, want true")
	}
	if got.AlbumID != m.AlbumID || got.AlbumName != m.AlbumName || got.Size != m.Size {
		t.Errorf("LoadManifest() = %+v, want album/name/size to match %+v", got, m)
	}
	if !reflect.DeepEqual(got.Assets, m.Assets) {
		t.Errorf("LoadManifest().Assets = %v, want %v", got.Assets, m.Assets)
	}
}

func TestLoadManifestMissingFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()

	m, existed, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v, want nil for a missing manifest", err)
	}
	if existed {
		t.Errorf("LoadManifest() existed = true, want false")
	}
	if m.Assets == nil {
		t.Errorf("LoadManifest() Assets = nil, want initialized empty map")
	}
	if len(m.Assets) != 0 {
		t.Errorf("LoadManifest() Assets = %v, want empty", m.Assets)
	}
}

func TestResizeGeometry(t *testing.T) {
	tests := []struct {
		width, height int
		want          string
	}{
		{0, 0, ""},
		{800, 0, "800x"},
		{0, 600, "x600"},
		{800, 600, "800x600"},
	}
	for _, tc := range tests {
		if got := resizeGeometry(tc.width, tc.height); got != tc.want {
			t.Errorf("resizeGeometry(%d, %d) = %q, want %q", tc.width, tc.height, got, tc.want)
		}
	}
}

func TestBuildImageMagickArgs(t *testing.T) {
	tests := []struct {
		name string
		opts ResizeOptions
		want []string
	}{
		{
			name: "width and height, explicit quality",
			opts: ResizeOptions{Width: 800, Height: 600, Quality: 70},
			want: []string{"src.png", "-resize", "800x600", "-quality", "70", "dst.jpg"},
		},
		{
			name: "width only",
			opts: ResizeOptions{Width: 800, Quality: 70},
			want: []string{"src.png", "-resize", "800x", "-quality", "70", "dst.jpg"},
		},
		{
			name: "no dimensions: quality-only re-encode",
			opts: ResizeOptions{Quality: 70},
			want: []string{"src.png", "-quality", "70", "dst.jpg"},
		},
		{
			name: "zero quality falls back to DefaultResizeQuality",
			opts: ResizeOptions{},
			want: []string{"src.png", "-quality", "85", "dst.jpg"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildImageMagickArgs("src.png", "dst.jpg", tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("BuildImageMagickArgs() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveImageMagickPath(t *testing.T) {
	if got, err := ResolveImageMagickPath("/custom/path/to/magick"); err != nil || got != "/custom/path/to/magick" {
		t.Errorf("ResolveImageMagickPath(explicit) = (%q, %v), want (\"/custom/path/to/magick\", nil)", got, err)
	}
}

func TestPlanAlbumSyncRefusesMismatchedManifest(t *testing.T) {
	album := immichapi.AlbumResponseDto{Id: openapi_types.UUID(uuid.New()), AlbumName: "Vacation"}

	tests := []struct {
		name     string
		manifest Manifest
	}{
		{
			name:     "different album",
			manifest: Manifest{AlbumID: uuid.New().String(), AlbumName: "Other Album"},
		},
		{
			name:     "different size",
			manifest: Manifest{AlbumID: album.Id.String(), Size: immichapi.Thumbnail},
		},
		{
			name:     "different resize setting",
			manifest: Manifest{AlbumID: album.Id.String(), Size: immichapi.Original, Resize: true},
		},
		{
			name:     "different timestamp-prefix setting",
			manifest: Manifest{AlbumID: album.Id.String(), Size: immichapi.Original, TimestampPrefix: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := SaveManifest(dir, tc.manifest); err != nil {
				t.Fatalf("SaveManifest() error = %v", err)
			}

			// These mismatches are all detected before any network call, so
			// a nil client is safe here.
			_, _, _, err := PlanAlbumSync(context.Background(), nil, album, dir, DownloadAlbumOptions{Size: immichapi.Original})
			if err == nil {
				t.Fatalf("PlanAlbumSync() error = nil, want a mismatch error")
			}
		})
	}
}
