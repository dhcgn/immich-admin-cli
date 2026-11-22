package workflows

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// SearchExcludingTrash posts a structured-shape metadata search (POST
// /search/metadata) with filter.trashedAt = {eq: null} added: unlike the
// legacy flat search, the structured filter includes trashed assets unless
// asked not to, and the generated DateFilterNullable cannot express an
// explicit null (omitempty), so it is patched into the JSON.
func SearchExcludingTrash(ctx context.Context, c *client.Client, body immichapi.MetadataSearchDto) (*immichapi.SearchAssetsResponse, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	filter, _ := m["filter"].(map[string]any)
	if filter == nil {
		filter = map[string]any{}
	}
	filter["trashedAt"] = map[string]any{"eq": nil}
	m["filter"] = filter
	if raw, err = json.Marshal(m); err != nil {
		return nil, err
	}
	return c.API.SearchAssetsWithBodyWithResponse(ctx, &immichapi.SearchAssetsParams{}, "application/json", bytes.NewReader(raw))
}
