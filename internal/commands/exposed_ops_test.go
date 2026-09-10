package commands

import (
	"testing"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func TestResolveAssetVisibility(t *testing.T) {
	for _, v := range []string{"archive", "hidden", "locked", "timeline"} {
		if _, err := resolveAssetVisibility(v); err != nil {
			t.Errorf("resolveAssetVisibility(%q) unexpected error: %v", v, err)
		}
	}
	if _, err := resolveAssetVisibility("bogus"); err == nil {
		t.Error("resolveAssetVisibility(bogus) expected error, got nil")
	}
}

func TestResolveAlbumUserRole(t *testing.T) {
	for _, r := range []string{"editor", "viewer", "owner"} {
		if _, err := resolveAlbumUserRole(r); err != nil {
			t.Errorf("resolveAlbumUserRole(%q) unexpected error: %v", r, err)
		}
	}
	if _, err := resolveAlbumUserRole("admin"); err == nil {
		t.Error("resolveAlbumUserRole(admin) expected error, got nil")
	}
}

func TestChunkUUIDs(t *testing.T) {
	ids := make([]openapi_types.UUID, 5)
	for i := range ids {
		ids[i] = openapi_types.UUID(uuid.New())
	}
	chunks := chunkUUIDs(ids, 2)
	if len(chunks) != 3 || len(chunks[0]) != 2 || len(chunks[1]) != 2 || len(chunks[2]) != 1 {
		t.Fatalf("chunkUUIDs(5, 2) = %d chunks with lens %v, want 3 chunks [2 2 1]", len(chunks), chunkLens(chunks))
	}
	if len(chunkUUIDs(nil, 500)) != 0 {
		t.Error("chunkUUIDs(nil) expected no chunks")
	}
}

func chunkLens(chunks [][]openapi_types.UUID) []int {
	out := make([]int, len(chunks))
	for i, c := range chunks {
		out[i] = len(c)
	}
	return out
}

func TestParseTagIDs(t *testing.T) {
	id := uuid.New().String()
	got, err := parseTagIDs([]string{id})
	if err != nil || len(got) != 1 || got[0].String() != id {
		t.Fatalf("parseTagIDs valid = %v, %v; want 1 id %s", got, err, id)
	}
	if _, err := parseTagIDs([]string{"not-a-uuid"}); err == nil {
		t.Error("parseTagIDs(invalid) expected error, got nil")
	}
}

func TestBulkIDError(t *testing.T) {
	msg := "duplicate"
	r := immichapi.BulkIdResponseDto{Id: openapi_types.UUID(uuid.New()), Success: false, ErrorMessage: &msg}
	if got := bulkIDError(r); got != msg {
		t.Errorf("bulkIDError = %q, want %q", got, msg)
	}
	r2 := immichapi.BulkIdResponseDto{Id: openapi_types.UUID(uuid.New()), Success: false}
	if got := bulkIDError(r2); got == "" {
		t.Error("bulkIDError without reason expected non-empty fallback")
	}
}
