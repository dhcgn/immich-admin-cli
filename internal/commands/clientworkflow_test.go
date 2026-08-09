package commands

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

func testAlbum(name string) immichapi.AlbumResponseDto {
	return immichapi.AlbumResponseDto{Id: openapi_types.UUID(uuid.New()), AlbumName: name, AssetCount: 1}
}

func albumNames(albums []immichapi.AlbumResponseDto) []string {
	out := make([]string, 0, len(albums))
	for _, a := range albums {
		out = append(out, a.AlbumName)
	}
	return out
}

func TestSelectAlbumsInteractively(t *testing.T) {
	user := immichapi.UserResponseDto{Id: openapi_types.UUID(uuid.New()), Name: "Julia", Email: "julia@example.com"}

	alreadyShared := testAlbum("Already Shared")
	alreadyShared.AlbumUsers = []immichapi.AlbumUserResponseDto{
		{User: user, Role: immichapi.AlbumUserRoleViewer},
	}

	albums := []immichapi.AlbumResponseDto{
		testAlbum("Amy's Wedding"),
		testAlbum("Amelia Birthday"),
		alreadyShared,
		testAlbum("Work Trip"),
	}

	tests := []struct {
		name   string
		answer string // one line per prompted (non-already-shared) album
		want   []string
	}{
		{
			name:   "all yes",
			answer: "y\ny\ny\n",
			want:   []string{"Amy's Wedding", "Amelia Birthday", "Already Shared", "Work Trip"},
		},
		{
			name:   "all no",
			answer: "n\nn\nn\n",
			want:   []string{"Already Shared"},
		},
		{
			name:   "mixed answers",
			answer: "y\nn\nyes\n",
			want:   []string{"Amy's Wedding", "Already Shared", "Work Trip"},
		},
		{
			name:   "empty line treated as no",
			answer: "\n\n\n",
			want:   []string{"Already Shared"},
		},
		{
			name:   "EOF stops asking and skips all remaining albums",
			answer: "y\n",
			want:   []string{"Amy's Wedding"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.NewReader(tc.answer)
			var out bytes.Buffer

			got := selectAlbumsInteractively(in, &out, albums, user, immichapi.AlbumUserRoleViewer)
			if !reflect.DeepEqual(albumNames(got), tc.want) {
				t.Errorf("selectAlbumsInteractively() = %v, want %v", albumNames(got), tc.want)
			}

			// Already-shared albums must never be prompted for.
			if strings.Contains(out.String(), "Already Shared") {
				t.Errorf("prompted for already-shared album, output: %q", out.String())
			}
		})
	}
}

func TestSelectAlbumsInteractivelyNoAlbums(t *testing.T) {
	user := immichapi.UserResponseDto{Id: openapi_types.UUID(uuid.New()), Name: "Julia"}
	var out bytes.Buffer

	got := selectAlbumsInteractively(strings.NewReader(""), &out, nil, user, immichapi.AlbumUserRoleViewer)
	if len(got) != 0 {
		t.Errorf("expected no albums selected, got %v", got)
	}
	if out.Len() != 0 {
		t.Errorf("expected no prompts printed, got %q", out.String())
	}
}

func testDateCheck(albumName string, kind workflows.PatternKind, mismatchCount int) workflows.AlbumDateCheck {
	var mismatches []immichapi.AssetResponseDto
	for range mismatchCount {
		mismatches = append(mismatches, immichapi.AssetResponseDto{Id: openapi_types.UUID(uuid.New())})
	}
	return workflows.AlbumDateCheck{
		Album:      immichapi.AlbumResponseDto{Id: openapi_types.UUID(uuid.New()), AlbumName: albumName},
		Pattern:    workflows.AlbumDatePattern{Kind: kind, From: time.Date(2025, 7, 4, 0, 0, 0, 0, time.UTC), To: time.Date(2025, 7, 5, 0, 0, 0, 0, time.UTC)},
		Mismatches: mismatches,
	}
}

func TestReviewAndFixAlbumDatesInteractively(t *testing.T) {
	clean := testDateCheck("Clean Album", workflows.PatternDay, 0)
	yearOnly := testDateCheck("2010 USA", workflows.PatternYear, 2)
	first := testDateCheck("First", workflows.PatternDay, 3)
	second := testDateCheck("Second", workflows.PatternDay, 1)

	tests := []struct {
		name      string
		answer    string
		wantFixed []string // album names actually passed to fix, in order
	}{
		{name: "all yes", answer: "y\ny\n", wantFixed: []string{"First", "Second"}},
		{name: "all no", answer: "n\nn\n", wantFixed: nil},
		{name: "mixed", answer: "y\nn\n", wantFixed: []string{"First"}},
		{name: "empty line treated as no", answer: "\n\n", wantFixed: nil},
		{name: "EOF stops asking and fixes nothing more", answer: "y\n", wantFixed: []string{"First"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checks := []workflows.AlbumDateCheck{clean, yearOnly, first, second}
			in := strings.NewReader(tc.answer)
			var out bytes.Buffer
			var fixedAlbums []string

			fix := func(_ context.Context, checks []workflows.AlbumDateCheck, _ workflows.FixAlbumDatesOptions) error {
				for _, c := range checks {
					fixedAlbums = append(fixedAlbums, c.Album.AlbumName)
				}
				return nil
			}

			err := reviewAndFixAlbumDatesInteractively(context.Background(), in, &out, checks, "https://example.com", workflows.FixAlbumDatesOptions{OffsetDays: 2}, fix)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(fixedAlbums, tc.wantFixed) {
				t.Errorf("fixed albums = %v, want %v", fixedAlbums, tc.wantFixed)
			}

			// A zero-mismatch album is never shown at all.
			if strings.Contains(out.String(), "Clean Album") {
				t.Errorf("printed a zero-mismatch album, output: %q", out.String())
			}
			// A year-only album is shown (report only) but never prompted or fixed.
			if !strings.Contains(out.String(), "2010 USA") {
				t.Errorf("expected report-only year album to still be shown, output: %q", out.String())
			}
			if strings.Contains(out.String(), `in "2010 USA"`) {
				t.Errorf("prompted for a year-only (report-only) album, output: %q", out.String())
			}
			// Each shown day-precision asset line includes its new date.
			if strings.Contains(out.String(), "First") && !strings.Contains(out.String(), "->") {
				t.Errorf("expected the new date to be shown alongside the old one, output: %q", out.String())
			}
		})
	}
}

func TestReviewAndFixAlbumDatesInteractivelyContinuesAfterFixError(t *testing.T) {
	first := testDateCheck("First", workflows.PatternDay, 1)
	second := testDateCheck("Second", workflows.PatternDay, 1)
	checks := []workflows.AlbumDateCheck{first, second}

	fix := func(_ context.Context, checks []workflows.AlbumDateCheck, _ workflows.FixAlbumDatesOptions) error {
		if checks[0].Album.AlbumName == "First" {
			return fmt.Errorf("boom")
		}
		return nil
	}

	var out bytes.Buffer
	err := reviewAndFixAlbumDatesInteractively(context.Background(), strings.NewReader("y\ny\n"), &out, checks, "https://example.com", workflows.FixAlbumDatesOptions{}, fix)
	if err == nil {
		t.Fatal("expected a summary error when one album fails to fix, got nil")
	}
	if !strings.Contains(out.String(), "Second") {
		t.Errorf("expected review to continue to the next album after a failure, output: %q", out.String())
	}
}

func TestFixableCount(t *testing.T) {
	checks := []workflows.AlbumDateCheck{
		testDateCheck("Day", workflows.PatternDay, 3),
		testDateCheck("Year", workflows.PatternYear, 5),
		testDateCheck("Clean", workflows.PatternDay, 0),
	}
	if got := fixableCount(checks); got != 3 {
		t.Errorf("fixableCount() = %d, want 3 (year-precision mismatches don't count)", got)
	}
}
