package commands

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func albumNamed(name string) immichapi.AlbumResponseDto {
	return immichapi.AlbumResponseDto{AlbumName: name}
}

func TestMatchTrimmedAlbumName(t *testing.T) {
	albums := []immichapi.AlbumResponseDto{
		albumNamed("USA "),
		albumNamed("Rome 2026"),
		albumNamed("  Padded  "),
	}
	tests := []struct {
		query string
		want  []string
	}{
		{query: "USA", want: []string{"USA "}},
		{query: "USA ", want: []string{"USA "}},
		{query: "  USA  ", want: []string{"USA "}},
		{query: "Rome 2026", want: []string{"Rome 2026"}},
		{query: "Padded", want: []string{"  Padded  "}},
		{query: "usa", want: nil},     // case still matters: whitespace only
		{query: "Unknown", want: nil}, // no match
		{query: "Rome", want: nil},    // substring is not a match
		{query: "2026", want: nil},    // substring is not a match
		{query: "", want: nil},        // empty query matches nothing here
	}
	for _, tc := range tests {
		got := matchTrimmedAlbumName(albums, tc.query)
		if len(got) != len(tc.want) {
			t.Errorf("matchTrimmedAlbumName(%q) = %v, want %v", tc.query, names(got), tc.want)
			continue
		}
		for i := range got {
			if got[i].AlbumName != tc.want[i] {
				t.Errorf("matchTrimmedAlbumName(%q)[%d] = %q, want %q", tc.query, i, got[i].AlbumName, tc.want[i])
			}
		}
	}

	multi := []immichapi.AlbumResponseDto{albumNamed("USA"), albumNamed("USA ")}
	if got := matchTrimmedAlbumName(multi, "USA"); len(got) != 2 {
		t.Errorf("matchTrimmedAlbumName ambiguous = %v, want both variants", names(got))
	}
}

func names(albums []immichapi.AlbumResponseDto) []string {
	out := make([]string, len(albums))
	for i, a := range albums {
		out[i] = a.AlbumName
	}
	return out
}

func TestWhitespaceNote(t *testing.T) {
	for name, want := range map[string]string{
		"USA ":  "trailing whitespace",
		" USA":  "leading whitespace",
		" USA ": "leading whitespace and trailing whitespace",
		"USA":   "whitespace",
		"US A":  "whitespace",
	} {
		if got := whitespaceNote(name); got != want {
			t.Errorf("whitespaceNote(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestOfferWhitespaceAlbum(t *testing.T) {
	origErr := errors.New(`no album named "USA" found`)
	candidate := immichapi.AlbumResponseDto{Id: openapi_types.UUID{1}, AlbumName: "USA "}

	// Accept on "y".
	var out bytes.Buffer
	got, err := offerWhitespaceAlbum(strings.NewReader("y\n"), &out, "USA", candidate, false, origErr)
	if err != nil || got.AlbumName != "USA " {
		t.Errorf("offer(yes) = (%q, %v), want (USA , nil)", got.AlbumName, err)
	}
	if !strings.Contains(out.String(), `"USA "`) || !strings.Contains(out.String(), "trailing whitespace") {
		t.Errorf("offer(yes) prompt = %q, want quoted name and whitespace note", out.String())
	}

	// Decline returns the original lookup error.
	out.Reset()
	if _, err := offerWhitespaceAlbum(strings.NewReader("n\n"), &out, "USA", candidate, false, origErr); err != origErr {
		t.Errorf("offer(no) error = %v, want the original error", err)
	}

	// Closed stdin declines too (scripts stay non-interactive).
	out.Reset()
	if _, err := offerWhitespaceAlbum(strings.NewReader(""), &out, "USA", candidate, false, origErr); err != origErr {
		t.Errorf("offer(eof) error = %v, want the original error", err)
	}

	// --yes auto-accepts without reading stdin.
	out.Reset()
	got, err = offerWhitespaceAlbum(strings.NewReader(""), &out, "USA", candidate, true, origErr)
	if err != nil || got.AlbumName != "USA " {
		t.Errorf("offer(autoYes) = (%q, %v), want (USA , nil)", got.AlbumName, err)
	}
}
