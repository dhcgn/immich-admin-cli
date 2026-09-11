//go:build integration

// Staging integration test for `albums rename` (PATCH /albums/{id}). It
// creates a throwaway album with a unique name, renames it twice (once by
// ID, once via the exact-name lookup that --album-name uses), verifies each
// rename with GET /albums/{id}, and deletes the album again. A t.Cleanup
// safety net deletes the album even if the test fails halfway.
//
// Run explicitly with:
//
//	go test -tags integration ./internal/commands/ -run AlbumRename
package commands

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func TestAlbumRenameRoundtripIntegration(t *testing.T) {
	c := stagingTestClient(t)
	ctx := context.Background()

	base := fmt.Sprintf("rename-test-%d", time.Now().UnixNano())
	orig := base + "-orig"
	renamed := base + "-renamed"
	final := base + "-final"

	create, err := c.API.CreateAlbumWithResponse(ctx, immichapi.CreateAlbumDto{AlbumName: orig})
	if err == nil {
		err = client.Check(create, http.StatusCreated)
	}
	if err != nil {
		t.Fatalf("CreateAlbum(%q): %v", orig, err)
	}
	if create.JSON201 == nil {
		t.Fatalf("CreateAlbum(%q): response had no body", orig)
	}
	id := create.JSON201.Id

	t.Cleanup(func() {
		resp, err := c.API.DeleteAlbumWithResponse(ctx, id)
		if err == nil {
			err = client.Check(resp, http.StatusNoContent)
		}
		if err != nil {
			t.Logf("cleanup: deleting album %s: %v", id, err)
		}
	})

	rename := func(name string) {
		t.Helper()
		resp, err := c.API.UpdateAlbumInfoWithResponse(ctx, id, immichapi.UpdateAlbumDto{AlbumName: &name})
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			t.Fatalf("UpdateAlbumInfo(%s -> %q): %v", id, name, err)
		}
		if resp.JSON200 == nil || resp.JSON200.AlbumName != name {
			t.Fatalf("UpdateAlbumInfo(%s -> %q): response name mismatch", id, name)
		}
		got, err := c.API.GetAlbumInfoWithResponse(ctx, id, &immichapi.GetAlbumInfoParams{})
		if err == nil {
			err = client.Check(got, http.StatusOK)
		}
		if err != nil {
			t.Fatalf("GetAlbumInfo(%s): %v", id, err)
		}
		if got.JSON200 == nil || got.JSON200.AlbumName != name || got.JSON200.Id != id {
			t.Fatalf("GetAlbumInfo(%s): want name %q, got %+v", id, name, got.JSON200)
		}
	}

	// 1. Rename by ID (the --album-id path).
	rename(renamed)

	// 2. Resolve via the exact-name lookup (the --album-name path), then rename.
	name := renamed
	list, err := c.API.GetAllAlbumsWithResponse(ctx, &immichapi.GetAllAlbumsParams{Name: &name})
	if err == nil {
		err = client.Check(list, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("GetAllAlbums(Name=%q): %v", name, err)
	}
	found := false
	if list.JSON200 != nil {
		for _, a := range *list.JSON200 {
			if a.Id == id {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("GetAllAlbums(Name=%q): created album %s not in results", name, id)
	}
	rename(final)

	t.Logf("renamed album %s %q -> %q -> %q", id, orig, renamed, final)
}
