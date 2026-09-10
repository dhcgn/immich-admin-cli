package workflows

import (
	"testing"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

//go:fix inline
func strptr(s string) *string { return new(s) }

func TestSearchPagerLegacy(t *testing.T) {
	var p SearchPager
	body := &immichapi.MetadataSearchDto{}

	p.Apply(body)
	if body.Page == nil || *body.Page != 1 || body.Cursor != nil {
		t.Fatalf("first Apply = page %v cursor %v, want page 1 cursor nil", body.Page, body.Cursor)
	}

	if !p.Next(immichapi.SearchAssetResponseDto{NextPage: new("2")}) {
		t.Fatal("Next with NextPage: got false, want true")
	}
	p.Apply(body)
	if body.Page == nil || *body.Page != 2 || body.Cursor != nil {
		t.Fatalf("second Apply = page %v cursor %v, want page 2 cursor nil", body.Page, body.Cursor)
	}

	if p.Next(immichapi.SearchAssetResponseDto{}) {
		t.Error("Next with no tokens: got true, want false")
	}
	if p.Pages() != 2 {
		t.Errorf("Pages() = %d, want 2", p.Pages())
	}
}

func TestSearchPagerCursorPreferred(t *testing.T) {
	var p SearchPager
	body := &immichapi.MetadataSearchDto{}

	p.Apply(body)
	// A v3.2 server answers with nextCursor (possibly alongside NextPage).
	if !p.Next(immichapi.SearchAssetResponseDto{NextPage: new("2"), NextCursor: new("abc")}) {
		t.Fatal("Next with NextCursor: got false, want true")
	}
	p.Apply(body)
	if body.Cursor == nil || *body.Cursor != "abc" || body.Page != nil {
		t.Fatalf("Apply after cursor = page %v cursor %v, want page nil cursor abc", body.Page, derefOrNil(body.Cursor))
	}

	// Empty cursor string falls back to the legacy token.
	if !p.Next(immichapi.SearchAssetResponseDto{NextPage: new("3"), NextCursor: new("")}) {
		t.Fatal("Next with empty cursor + NextPage: got false, want true")
	}
	p.Apply(body)
	if body.Page == nil || *body.Page != 3 || body.Cursor != nil {
		t.Fatalf("Apply after fallback = page %v cursor %v, want page 3 cursor nil", body.Page, body.Cursor)
	}

	// An unparseable legacy token stops rather than loops.
	if p.Next(immichapi.SearchAssetResponseDto{NextPage: new("not-a-number")}) {
		t.Error("Next with garbage NextPage: got true, want false")
	}
}

func derefOrNil(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
