package workflows

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	return p
}

func TestAnalyzeJPEG(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantSOI bool
		wantEOI bool
	}{
		{"valid", []byte{0xFF, 0xD8, 0x11, 0x22, 0xFF, 0xD9}, true, true},
		{"missing-eoi", []byte{0xFF, 0xD8, 0x11, 0x22, 0x33}, true, false},
		{"missing-soi", []byte{0x00, 0x11, 0x22, 0xFF, 0xD9}, false, true},
		{"neither", []byte{0x00, 0x11, 0x22, 0x33}, false, false},
		{"too-short", []byte{0xFF}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := writeTemp(t, "in.jpg", tc.data)
			a, err := analyzeJPEG(p)
			if err != nil {
				t.Fatalf("analyzeJPEG: %v", err)
			}
			if a.HasSOI != tc.wantSOI {
				t.Errorf("HasSOI = %v, want %v", a.HasSOI, tc.wantSOI)
			}
			if a.HasEOI != tc.wantEOI {
				t.Errorf("HasEOI = %v, want %v", a.HasEOI, tc.wantEOI)
			}
			if a.Size != int64(len(tc.data)) {
				t.Errorf("Size = %d, want %d", a.Size, len(tc.data))
			}
		})
	}
}

func TestMarkerStrategyApplicable(t *testing.T) {
	s := markerStrategy{}
	if s.Name() != "marker" {
		t.Errorf("Name = %q, want marker", s.Name())
	}
	cases := []struct {
		a    JPEGAnalysis
		want bool
	}{
		{JPEGAnalysis{HasSOI: true, HasEOI: false}, true},   // the target case
		{JPEGAnalysis{HasSOI: true, HasEOI: true}, false},   // already OK
		{JPEGAnalysis{HasSOI: false, HasEOI: false}, false}, // not a JPEG start
		{JPEGAnalysis{HasSOI: false, HasEOI: true}, false},
	}
	for _, tc := range cases {
		if got := s.Applicable(tc.a); got != tc.want {
			t.Errorf("Applicable(%+v) = %v, want %v", tc.a, got, tc.want)
		}
	}
}

func TestMarkerStrategyRepair(t *testing.T) {
	// A JPEG missing its EOI marker; repair must append FF D9 and leave the
	// original bytes untouched (append-only, EXIF-preserving).
	orig := []byte{0xFF, 0xD8, 0xDE, 0xAD, 0xBE, 0xEF}
	src := writeTemp(t, "src.jpg", orig)
	dst := filepath.Join(t.TempDir(), "dst.jpg")

	if err := (markerStrategy{}).Repair(src, dst); err != nil {
		t.Fatalf("Repair: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading repaired file: %v", err)
	}
	want := append(append([]byte{}, orig...), 0xFF, 0xD9)
	if len(got) != len(want) {
		t.Fatalf("repaired length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("repaired byte %d = %#x, want %#x", i, got[i], want[i])
		}
	}

	// The repaired file must now analyse as a complete JPEG.
	a, err := analyzeJPEG(dst)
	if err != nil {
		t.Fatalf("analyzeJPEG(repaired): %v", err)
	}
	if !a.HasSOI || !a.HasEOI {
		t.Errorf("repaired file HasSOI=%v HasEOI=%v, want both true", a.HasSOI, a.HasEOI)
	}

	// The source must be unchanged.
	srcAfter, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}
	if len(srcAfter) != len(orig) {
		t.Errorf("source was modified: len %d, want %d", len(srcAfter), len(orig))
	}
}

func TestParseRepairMode(t *testing.T) {
	valid := map[string]RepairMode{
		"marker": RepairModeMarker,
		"all":    RepairModeAll,
	}
	for in, want := range valid {
		got, err := ParseRepairMode(in)
		if err != nil {
			t.Errorf("ParseRepairMode(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("ParseRepairMode(%q) = %q, want %q", in, got, want)
		}
	}

	for _, in := range []string{"", "MARKER", "reencode", "bogus"} {
		if _, err := ParseRepairMode(in); err == nil {
			t.Errorf("ParseRepairMode(%q) expected error, got nil", in)
		}
	}
}

func TestStrategiesForMode(t *testing.T) {
	if got := strategiesForMode(RepairModeMarker); len(got) != 1 || got[0].Name() != "marker" {
		t.Errorf("strategiesForMode(marker) = %v, want [marker]", got)
	}
	if got := strategiesForMode(RepairModeAll); len(got) != len(repairStrategies) {
		t.Errorf("strategiesForMode(all) len = %d, want %d", len(got), len(repairStrategies))
	}
}

func TestIsJPEGName(t *testing.T) {
	jpeg := []string{"a.jpg", "b.JPEG", "c.Jpe", "d.jfif"}
	notJpeg := []string{"a.png", "b.mp4", "c", "d.tiff"}
	for _, n := range jpeg {
		if !isJPEGName(n) {
			t.Errorf("isJPEGName(%q) = false, want true", n)
		}
	}
	for _, n := range notJpeg {
		if isJPEGName(n) {
			t.Errorf("isJPEGName(%q) = true, want false", n)
		}
	}
}
