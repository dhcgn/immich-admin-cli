package workflows

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func TestParseAlbumDatePattern(t *testing.T) {
	tests := []struct {
		name      string
		album     string
		wantMatch bool
		wantKind  PatternKind
		wantFrom  string // "2006-01-02"
		wantTo    string
	}{
		{
			name:      "day with title",
			album:     "2025-07-04 Garten",
			wantMatch: true,
			wantKind:  PatternDay,
			wantFrom:  "2025-07-04",
			wantTo:    "2025-07-05",
		},
		{
			name:      "exact day, no title",
			album:     "2025-07-04",
			wantMatch: true,
			wantKind:  PatternDay,
			wantFrom:  "2025-07-04",
			wantTo:    "2025-07-05",
		},
		{
			name:      "year with title",
			album:     "2010 USA",
			wantMatch: true,
			wantKind:  PatternYear,
			wantFrom:  "2010-01-01",
			wantTo:    "2011-01-01",
		},
		{
			name:      "exact year, no title",
			album:     "2010",
			wantMatch: true,
			wantKind:  PatternYear,
			wantFrom:  "2010-01-01",
			wantTo:    "2011-01-01",
		},
		{
			name:      "invalid calendar date is no match",
			album:     "2025-13-40 Foo",
			wantMatch: false,
		},
		{
			name:      "unrelated name is no match",
			album:     "Family Vacation",
			wantMatch: false,
		},
		{
			name:      "day-pattern name is not misclassified as year",
			album:     "2025-07-04 Garten",
			wantMatch: true,
			wantKind:  PatternDay,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseAlbumDatePattern(tt.album)
			if ok != tt.wantMatch {
				t.Fatalf("parseAlbumDatePattern(%q) match = %v, want %v", tt.album, ok, tt.wantMatch)
			}
			if !tt.wantMatch {
				return
			}
			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if tt.wantFrom != "" {
				wantFrom, _ := time.ParseInLocation("2006-01-02", tt.wantFrom, time.UTC)
				if !got.From.Equal(wantFrom) {
					t.Errorf("From = %v, want %v", got.From, wantFrom)
				}
			}
			if tt.wantTo != "" {
				wantTo, _ := time.ParseInLocation("2006-01-02", tt.wantTo, time.UTC)
				if !got.To.Equal(wantTo) {
					t.Errorf("To = %v, want %v", got.To, wantTo)
				}
			}
		})
	}
}

func testAssetWithLocalDateTime(name string, t time.Time) immichapi.AssetResponseDto {
	return immichapi.AssetResponseDto{
		Id:               openapi_types.UUID(uuid.New()),
		OriginalFileName: name,
		LocalDateTime:    t,
	}
}

func TestFindDateMismatches(t *testing.T) {
	pattern := AlbumDatePattern{
		Kind: PatternDay,
		From: time.Date(2025, 7, 4, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 7, 5, 0, 0, 0, 0, time.UTC),
	}

	inside := testAssetWithLocalDateTime("inside.jpg", time.Date(2025, 7, 4, 12, 0, 0, 0, time.UTC))
	atFrom := testAssetWithLocalDateTime("at-from.jpg", pattern.From)
	atTo := testAssetWithLocalDateTime("at-to.jpg", pattern.To)
	before := testAssetWithLocalDateTime("before.jpg", time.Date(2025, 7, 3, 23, 59, 59, 0, time.UTC))
	after := testAssetWithLocalDateTime("after.jpg", time.Date(2025, 7, 5, 0, 0, 1, 0, time.UTC))

	assets := []immichapi.AssetResponseDto{inside, atFrom, atTo, before, after}

	got := findDateMismatches(assets, pattern, 0)

	wantNames := map[string]bool{"at-to.jpg": true, "before.jpg": true, "after.jpg": true}
	if len(got) != len(wantNames) {
		t.Fatalf("got %d mismatches, want %d: %v", len(got), len(wantNames), got)
	}
	for _, a := range got {
		if !wantNames[a.OriginalFileName] {
			t.Errorf("unexpected mismatch: %s", a.OriginalFileName)
		}
	}
}

func TestFindDateMismatchesWithOffset(t *testing.T) {
	pattern := AlbumDatePattern{
		Kind: PatternDay,
		From: time.Date(2025, 7, 4, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 7, 5, 0, 0, 0, 0, time.UTC),
	}

	oneDayBefore := testAssetWithLocalDateTime("one-day-before.jpg", time.Date(2025, 7, 3, 12, 0, 0, 0, time.UTC))
	oneDayAfter := testAssetWithLocalDateTime("one-day-after.jpg", time.Date(2025, 7, 5, 12, 0, 0, 0, time.UTC))
	twoDaysBefore := testAssetWithLocalDateTime("two-days-before.jpg", time.Date(2025, 7, 2, 12, 0, 0, 0, time.UTC))
	twoDaysAfter := testAssetWithLocalDateTime("two-days-after.jpg", time.Date(2025, 7, 6, 12, 0, 0, 0, time.UTC))

	assets := []immichapi.AssetResponseDto{oneDayBefore, oneDayAfter, twoDaysBefore, twoDaysAfter}

	// With no offset, everything a day or more outside the range mismatches.
	got := findDateMismatches(assets, pattern, 0)
	if len(got) != 4 {
		t.Fatalf("offset=0: got %d mismatches, want 4", len(got))
	}

	// With a 1-day offset, the ones exactly 1 day outside are forgiven; the
	// ones 2 days outside still mismatch.
	got = findDateMismatches(assets, pattern, 24*time.Hour)
	wantNames := map[string]bool{"two-days-before.jpg": true, "two-days-after.jpg": true}
	if len(got) != len(wantNames) {
		t.Fatalf("offset=1 day: got %d mismatches, want %d: %v", len(got), len(wantNames), got)
	}
	for _, a := range got {
		if !wantNames[a.OriginalFileName] {
			t.Errorf("unexpected mismatch with offset: %s", a.OriginalFileName)
		}
	}
}

func TestComputeFixedDateTime(t *testing.T) {
	dayPattern := AlbumDatePattern{
		Kind: PatternDay,
		From: time.Date(2025, 7, 4, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 7, 5, 0, 0, 0, 0, time.UTC),
	}
	asset := testAssetWithLocalDateTime("a.jpg", time.Date(2025, 7, 9, 14, 23, 1, 0, time.UTC))

	fixed, ok := ComputeFixedDateTime(asset, dayPattern)
	if !ok {
		t.Fatalf("ComputeFixedDateTime: ok = false, want true for day pattern")
	}
	want := time.Date(2025, 7, 4, 14, 23, 1, 0, time.UTC)
	if !fixed.Equal(want) {
		t.Errorf("fixed = %v, want %v", fixed, want)
	}

	yearPattern := AlbumDatePattern{
		Kind: PatternYear,
		From: time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2011, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if _, ok := ComputeFixedDateTime(asset, yearPattern); ok {
		t.Errorf("ComputeFixedDateTime: ok = true, want false for year pattern (report-only)")
	}
}

func TestMaxDeviation(t *testing.T) {
	pattern := AlbumDatePattern{
		Kind: PatternDay,
		From: time.Date(2025, 7, 4, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 7, 5, 0, 0, 0, 0, time.UTC),
	}

	t.Run("no mismatches is zero", func(t *testing.T) {
		got := maxDeviation(AlbumDateCheck{Pattern: pattern})
		if got != 0 {
			t.Errorf("maxDeviation() = %v, want 0", got)
		}
	})

	t.Run("takes the worst of several mismatches", func(t *testing.T) {
		check := AlbumDateCheck{
			Pattern: pattern,
			Mismatches: []immichapi.AssetResponseDto{
				testAssetWithLocalDateTime("close.jpg", time.Date(2025, 7, 3, 12, 0, 0, 0, time.UTC)), // 12h before
				testAssetWithLocalDateTime("far.jpg", time.Date(2025, 4, 27, 12, 0, 0, 0, time.UTC)),  // ~68 days before
				testAssetWithLocalDateTime("after.jpg", time.Date(2025, 7, 6, 0, 0, 0, 0, time.UTC)),  // 1 day after
			},
		}
		want := pattern.From.Sub(time.Date(2025, 4, 27, 12, 0, 0, 0, time.UTC))
		if got := maxDeviation(check); got != want {
			t.Errorf("maxDeviation() = %v, want %v", got, want)
		}
	})
}

func TestCheckAlbumDatesOrdersWorstFirst(t *testing.T) {
	pattern := AlbumDatePattern{
		Kind: PatternDay,
		From: time.Date(2025, 7, 4, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 7, 5, 0, 0, 0, 0, time.UTC),
	}
	clean := AlbumDateCheck{Album: immichapi.AlbumResponseDto{AlbumName: "Clean"}, Pattern: pattern}
	mild := AlbumDateCheck{
		Album:      immichapi.AlbumResponseDto{AlbumName: "Mild"},
		Pattern:    pattern,
		Mismatches: []immichapi.AssetResponseDto{testAssetWithLocalDateTime("a.jpg", time.Date(2025, 7, 6, 0, 0, 0, 0, time.UTC))},
	}
	severe := AlbumDateCheck{
		Album:      immichapi.AlbumResponseDto{AlbumName: "Severe"},
		Pattern:    pattern,
		Mismatches: []immichapi.AssetResponseDto{testAssetWithLocalDateTime("b.jpg", time.Date(2025, 4, 27, 0, 0, 0, 0, time.UTC))},
	}

	checks := []AlbumDateCheck{clean, mild, severe}
	sort.SliceStable(checks, func(i, j int) bool { return maxDeviation(checks[i]) > maxDeviation(checks[j]) })

	got := []string{checks[0].Album.AlbumName, checks[1].Album.AlbumName, checks[2].Album.AlbumName}
	want := []string{"Severe", "Mild", "Clean"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
