package workflows

import "testing"

func TestIsHEICFileName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"IMG_2854.heic", true},
		{"IMG_2854.HEIC", true},
		{"photo.heif", true},
		{"IMG_5524.HEIC.jpg", false}, // real file name is a JPEG, not HEIC
		{"photo.jpg", false},
		{"noext", false},
	}
	for _, tc := range tests {
		if got := isHEICFileName(tc.name); got != tc.want {
			t.Errorf("isHEICFileName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHasHEICTileDefect(t *testing.T) {
	// Dimensions below are real assets from a production library, confirmed by
	// independently decoding both the original (fine) and Immich's generated
	// preview (garbled for the "defect" cases) — see findheictiledefect.go.
	tests := []struct {
		name          string
		width, height int
		want          bool
	}{
		{"canon-40d-converted-via-zoner", 3888, 2592, true},
		{"canon-350d-converted-via-zoner", 3456, 2304, true},
		{"nikon-d750-converted-via-dxo", 2726, 1820, true},
		{"nikon-d750-converted-via-dxo-2", 6016, 4016, true},
		{"old-scan-collection", 4416, 3312, true},
		{"oppo-native-heic", 3072, 4096, false},
		{"oppo-native-heic-2", 4096, 3072, false},
		{"exact-multiple-both", 1024, 512, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasHEICTileDefect(tc.width, tc.height, defaultHEICGridTileSize); got != tc.want {
				t.Errorf("hasHEICTileDefect(%d, %d, %d) = %v, want %v", tc.width, tc.height, defaultHEICGridTileSize, got, tc.want)
			}
		})
	}
}

func TestHasHEICTileDefectTileSizeFallback(t *testing.T) {
	// tileSize <= 0 must fall back to defaultHEICGridTileSize, not divide by zero.
	if got := hasHEICTileDefect(3888, 2592, 0); got != true {
		t.Errorf("hasHEICTileDefect with tileSize=0 = %v, want true (falls back to default 512)", got)
	}
}
