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
		"marker":    RepairModeMarker,
		"tiff-tags": RepairModeTIFFTags,
		"all":       RepairModeAll,
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
	if got := strategiesForMode(RepairModeTIFFTags); len(got) != 0 {
		t.Errorf("strategiesForMode(tiff-tags) = %v, want empty (no JPEG strategy applies)", got)
	}
	if got := strategiesForMode(RepairModeAll); len(got) != len(repairStrategies) {
		t.Errorf("strategiesForMode(all) len = %d, want %d", len(got), len(repairStrategies))
	}
}

func TestTIFFStrategiesForMode(t *testing.T) {
	if got := tiffStrategiesForMode(RepairModeMarker); len(got) != 0 {
		t.Errorf("tiffStrategiesForMode(marker) = %v, want empty (no TIFF strategy applies)", got)
	}
	if got := tiffStrategiesForMode(RepairModeTIFFTags); len(got) != 1 || got[0].Name() != "tiff-zero-count" {
		t.Errorf("tiffStrategiesForMode(tiff-tags) = %v, want [tiff-zero-count]", got)
	}
	if got := tiffStrategiesForMode(RepairModeAll); len(got) != len(tiffRepairStrategies) {
		t.Errorf("tiffStrategiesForMode(all) len = %d, want %d", len(got), len(tiffRepairStrategies))
	}
}

// buildTIFF assembles a minimal single-IFD TIFF file (little-endian). tags is
// a list of {tagID, type, count} triples; each entry's value/offset field is
// set to 0 (irrelevant for the zero-count detection under test). Returns the
// full file bytes plus the absolute offsets of each entry's count field, in
// the same order as tags, for assertions against TIFFZeroCountTag.
func buildTIFF(t *testing.T, tags [][3]uint32) ([]byte, []int64) {
	t.Helper()
	n := len(tags)
	ifdOffset := int64(8)
	buf := make([]byte, 0, 8+2+n*12+4)

	// Header: little-endian, magic 42, first IFD at offset 8.
	buf = append(buf, 'I', 'I')
	buf = append(buf, 42, 0)
	buf = append(buf, 8, 0, 0, 0)

	// IFD entry count.
	buf = append(buf, byte(n), byte(n>>8))

	var countOffsets []int64
	for _, tag := range tags {
		entryStart := int64(len(buf))
		tagID, typ, count := uint16(tag[0]), uint16(tag[1]), tag[2]
		buf = append(buf, byte(tagID), byte(tagID>>8))
		buf = append(buf, byte(typ), byte(typ>>8))
		countOffsets = append(countOffsets, entryStart+4)
		buf = append(buf, byte(count), byte(count>>8), byte(count>>16), byte(count>>24))
		buf = append(buf, 0, 0, 0, 0) // value/offset field, unused by the detector
	}

	// Next-IFD offset: 0 (end of chain).
	buf = append(buf, 0, 0, 0, 0)

	_ = ifdOffset
	return buf, countOffsets
}

func TestAnalyzeTIFF(t *testing.T) {
	t.Run("clean tiff has no zero-count tags", func(t *testing.T) {
		data, _ := buildTIFF(t, [][3]uint32{
			{0x0100, 4, 1}, // ImageWidth, LONG, count=1
			{0x0101, 4, 1}, // ImageLength, LONG, count=1
		})
		p := writeTemp(t, "clean.tif", data)
		a, err := analyzeTIFF(p)
		if err != nil {
			t.Fatalf("analyzeTIFF: %v", err)
		}
		if !a.Valid {
			t.Fatal("Valid = false, want true")
		}
		if len(a.ZeroCountTags) != 0 {
			t.Fatalf("ZeroCountTags = %v, want empty", a.ZeroCountTags)
		}
	})

	t.Run("recognizes the exact zero-count defect", func(t *testing.T) {
		data, offsets := buildTIFF(t, [][3]uint32{
			{0x0100, 4, 1},
			{0x8657, 1, 0}, // the real-world defect: BYTE field, count=0
			{0x8658, 2, 0}, // second observed defect: ASCII field, count=0
		})
		p := writeTemp(t, "bad.tif", data)
		a, err := analyzeTIFF(p)
		if err != nil {
			t.Fatalf("analyzeTIFF: %v", err)
		}
		if !a.Valid {
			t.Fatal("Valid = false, want true (chain must still parse cleanly)")
		}
		if len(a.ZeroCountTags) != 2 {
			t.Fatalf("ZeroCountTags = %v, want 2 entries", a.ZeroCountTags)
		}
		if a.ZeroCountTags[0].Tag != 0x8657 || a.ZeroCountTags[0].CountFieldOffset != offsets[1] {
			t.Errorf("ZeroCountTags[0] = %+v, want Tag=0x8657 offset=%d", a.ZeroCountTags[0], offsets[1])
		}
		if a.ZeroCountTags[1].Tag != 0x8658 || a.ZeroCountTags[1].CountFieldOffset != offsets[2] {
			t.Errorf("ZeroCountTags[1] = %+v, want Tag=0x8658 offset=%d", a.ZeroCountTags[1], offsets[2])
		}
	})

	t.Run("not a TIFF at all", func(t *testing.T) {
		p := writeTemp(t, "not.tif", []byte{0xFF, 0xD8, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05})
		a, err := analyzeTIFF(p)
		if err != nil {
			t.Fatalf("analyzeTIFF: %v", err)
		}
		if a.Valid {
			t.Error("Valid = true, want false for a non-TIFF header")
		}
		if len(a.ZeroCountTags) != 0 {
			t.Errorf("ZeroCountTags = %v, want empty when Valid is false", a.ZeroCountTags)
		}
	})

	t.Run("big-endian tiff is also recognized", func(t *testing.T) {
		// Same as buildTIFF but big-endian ("MM"), single defective tag.
		buf := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1}
		// One entry: tag 0x8657, type 1 (BYTE), count 0, value 0.
		buf = append(buf, 0x86, 0x57, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0)
		buf = append(buf, 0, 0, 0, 0) // next IFD = 0
		p := writeTemp(t, "be.tif", buf)
		a, err := analyzeTIFF(p)
		if err != nil {
			t.Fatalf("analyzeTIFF: %v", err)
		}
		if !a.Valid || len(a.ZeroCountTags) != 1 || a.ZeroCountTags[0].Tag != 0x8657 {
			t.Fatalf("got Valid=%v tags=%v, want 1 zero-count tag 0x8657", a.Valid, a.ZeroCountTags)
		}
	})
}

func TestTIFFZeroCountStrategyApplicable(t *testing.T) {
	s := tiffZeroCountStrategy{}
	if s.Name() != "tiff-zero-count" {
		t.Errorf("Name = %q, want tiff-zero-count", s.Name())
	}
	cases := []struct {
		a    TIFFAnalysis
		want bool
	}{
		{TIFFAnalysis{Valid: true, ZeroCountTags: []TIFFZeroCountTag{{Tag: 0x8657}}}, true},
		{TIFFAnalysis{Valid: true, ZeroCountTags: nil}, false},                           // clean TIFF: not applicable
		{TIFFAnalysis{Valid: false, ZeroCountTags: nil}, false},                          // unparseable: not applicable
		{TIFFAnalysis{Valid: false, ZeroCountTags: []TIFFZeroCountTag{{Tag: 1}}}, false}, // defensive: never trust tags when !Valid
	}
	for _, tc := range cases {
		if got := s.Applicable(tc.a); got != tc.want {
			t.Errorf("Applicable(%+v) = %v, want %v", tc.a, got, tc.want)
		}
	}
}

func TestTIFFZeroCountStrategyRepair(t *testing.T) {
	data, _ := buildTIFF(t, [][3]uint32{
		{0x0100, 4, 1},
		{0x8657, 1, 0},
		{0x8658, 2, 0},
	})
	src := writeTemp(t, "src.tif", data)
	dst := filepath.Join(t.TempDir(), "dst.tif")

	if err := (tiffZeroCountStrategy{}).Repair(src, dst); err != nil {
		t.Fatalf("Repair: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading repaired file: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("repaired length = %d, want %d (layout must be preserved)", len(got), len(data))
	}

	after, err := analyzeTIFF(dst)
	if err != nil {
		t.Fatalf("analyzeTIFF(repaired): %v", err)
	}
	if !after.Valid {
		t.Fatal("repaired file: Valid = false, want true")
	}
	if len(after.ZeroCountTags) != 0 {
		t.Errorf("repaired file still has zero-count tags: %v", after.ZeroCountTags)
	}

	// The source must be unchanged.
	srcAfter, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}
	if string(srcAfter) != string(data) {
		t.Error("source was modified")
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
