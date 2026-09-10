package workflows

import (
	"strconv"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// SearchPager tracks POST /search/metadata pagination across requests. It
// speaks both the cursor scheme (cursor/nextCursor, Immich v3.2+, where the
// page/nextPage fields are deprecated) and the legacy page scheme
// (page/nextPage, older servers): the cursor is preferred whenever the
// server returns one, otherwise the numeric legacy token is used. The zero
// value starts at page 1. Exported because the search command
// (internal/commands) pages the same endpoint.
type SearchPager struct {
	page   int
	cursor *string
	pages  int
}

// Apply sets the request body's pagination fields from the pager state —
// the cursor when known, otherwise the current page — clearing the other
// field so a server never sees both. It counts the request for Pages.
func (p *SearchPager) Apply(body *immichapi.MetadataSearchDto) {
	if p.page <= 0 {
		p.page = 1
	}
	p.pages++
	if p.cursor != nil {
		body.Cursor = p.cursor
		body.Page = nil
		return
	}
	body.Page = &p.page
	body.Cursor = nil
}

// Next advances the pager from a response's asset listing, reporting whether
// another request should follow. An unparseable legacy token stops the
// listing rather than risking a loop.
func (p *SearchPager) Next(assets immichapi.SearchAssetResponseDto) bool {
	if assets.NextCursor != nil && *assets.NextCursor != "" {
		p.cursor = assets.NextCursor
		return true
	}
	p.cursor = nil
	if assets.NextPage == nil || *assets.NextPage == "" {
		return false
	}
	next, err := strconv.Atoi(*assets.NextPage)
	if err != nil {
		return false
	}
	p.page = next
	return true
}

// Pages reports how many requests Apply has prepared — for status footers,
// since cursor listings have no server page numbering to display.
func (p *SearchPager) Pages() int {
	return p.pages
}
