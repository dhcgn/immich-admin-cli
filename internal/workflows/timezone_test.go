package workflows

import (
	"slices"
	"testing"
	"time"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// tzAsset builds an asset with the given zone (nil = none), wall clock and
// UTC offset between localDateTime and fileCreatedAt.
func tzAsset(tz *string, wall string, offset time.Duration) immichapi.AssetResponseDto {
	l, _ := time.Parse(time.RFC3339, wall)
	dto := l.Add(-offset)
	return immichapi.AssetResponseDto{
		OriginalFileName: "x.jpg",
		LocalDateTime:    l,
		FileCreatedAt:    l.Add(-offset),
		ExifInfo:         &immichapi.ExifResponseDto{TimeZone: tz, DateTimeOriginal: &dto},
	}
}

//go:fix inline
func sp(s string) *string { return new(s) }

func TestParseFixedOffset(t *testing.T) {
	for in, want := range map[string]int{"UTC+2": 7200, "UTC-5": -18000, "UTC+5:30": 19800, "UTC+0": 0, "UTC-03:30": -12600} {
		if got, ok := parseFixedOffset(in); !ok || got != want {
			t.Errorf("parseFixedOffset(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"UTC", "Europe/Berlin", "UTC+", "GMT+1"} {
		if _, ok := parseFixedOffset(in); ok {
			t.Errorf("parseFixedOffset(%q) accepted", in)
		}
	}
}

func TestClassifyTZ(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	h := time.Hour
	cases := []struct {
		name string
		a    immichapi.AssetResponseDto
		ref  bool
		want []string
	}{
		{"none", tzAsset(nil, "2005-06-01T12:00:00Z", 0), true, []string{TZClassNone}},
		{"utc", tzAsset(new("UTC"), "2005-06-01T12:00:00Z", 0), true, []string{TZClassUTC}},
		{"offset ok summer", tzAsset(new("UTC+2"), "2024-07-01T12:00:00Z", 2*h), true, []string{TZClassOffset}},
		{"dst winter with summer offset", tzAsset(new("UTC+2"), "2024-01-10T12:00:00Z", 2*h), true, []string{TZClassOffset, TZClassDSTMismatch}},
		{"dst summer with winter offset", tzAsset(new("UTC+1"), "2024-07-10T12:00:00Z", 1*h), true, []string{TZClassOffset, TZClassDSTMismatch}},
		{"foreign offset not a dst mismatch", tzAsset(new("UTC-5"), "2024-01-10T12:00:00Z", -5*h), true, []string{TZClassOffset}},
		{"no ref no dst class", tzAsset(new("UTC+2"), "2024-01-10T12:00:00Z", 2*h), false, []string{TZClassOffset}},
		{"iana consistent", tzAsset(new("Europe/Berlin"), "2024-07-01T12:00:00Z", 2*h), true, []string{TZClassIANA}},
		{"iana inconsistent", tzAsset(new("Europe/Berlin"), "2024-07-01T12:00:00Z", 0), true, []string{TZClassIANA, TZClassIANAInconsistent}},
		{"no exif", immichapi.AssetResponseDto{}, true, []string{TZClassNone}},
	}
	for _, c := range cases {
		var ref *time.Location
		if c.ref {
			ref = berlin
		}
		if got := ClassifyTZ(c.a, ref); !slices.Equal(got, c.want) {
			t.Errorf("%s: ClassifyTZ = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTZSetDateKeepsWallClockAcrossDST(t *testing.T) {
	berlin, _ := LoadIANAZone("Europe/Berlin")
	for wall, want := range map[string]string{
		"2020-07-15T12:00:00Z":    "2020-07-15T12:00:00+02:00",
		"2020-01-15T12:00:00Z":    "2020-01-15T12:00:00+01:00",
		"2020-03-29T03:30:00Z":    "2020-03-29T03:30:00+02:00", // right after spring-forward
		"2020-07-15T12:00:00.08Z": "2020-07-15T12:00:00.08+02:00",
		"1983-05-01T08:15:00Z":    "1983-05-01T08:15:00+02:00",
		"1979-05-01T08:15:00Z":    "1979-05-01T08:15:00+01:00", // no DST in West Germany before 1980
	} {
		got, err := TZSetDate(tzAsset(nil, wall, 0), berlin)
		if err != nil || got != want {
			t.Errorf("TZSetDate(%s) = %q, %v; want %q", wall, got, err, want)
		}
	}
}

func TestSplitTZSetNeverTouchesZonedOrUndatedAssets(t *testing.T) {
	berlin, _ := LoadIANAZone("Europe/Berlin")
	undated := tzAsset(nil, "2020-01-01T00:00:00Z", 0)
	undated.ExifInfo.DateTimeOriginal = nil
	todo, skips := SplitTZSet([]immichapi.AssetResponseDto{
		tzAsset(nil, "2020-01-01T00:00:00Z", 0),
		tzAsset(new("UTC"), "2020-01-01T00:00:00Z", 0),
		tzAsset(new("UTC+1"), "2020-01-01T00:00:00Z", time.Hour),
		undated,
		{OriginalFileName: "noexif.jpg"},
	}, berlin)
	if len(todo) != 1 || len(skips) != 4 {
		t.Fatalf("todo=%d skips=%d (%v), want 1 and 4", len(todo), len(skips), skips)
	}
}

func TestLoadIANAZone(t *testing.T) {
	for _, ok := range []string{"Europe/Berlin", "America/Chicago", "UTC"} {
		if _, err := LoadIANAZone(ok); err != nil {
			t.Errorf("LoadIANAZone(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Local", "UTC+1", "Europe/Nowhere", "CEST"} {
		if _, err := LoadIANAZone(bad); err == nil {
			t.Errorf("LoadIANAZone(%q) accepted", bad)
		}
	}
}
