//go:build integration

// This file holds a read-only integration test that talks to a real Immich
// server configured in config.prod.yaml. It is excluded from the normal test
// build (and CI) by the `integration` build tag; run it explicitly with:
//
//	go test -tags integration ./internal/workflows/ -run AddUsersToAlbum
//
// It only reads (SelectAlbumsForSharing, ResolveUser) and never shares an
// album (ShareAlbumsWithUser is never called).
package workflows

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/config"
)

func TestAddUsersToAlbumSelectIntegration(t *testing.T) {
	if _, err := os.Stat(configPath); err != nil {
		t.Skipf("skipping: %s not found (%v)", filepath.Clean(configPath), err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}

	// Match-all include; this is a read-only call — it must not share anything.
	opts := AddUsersToAlbumOptions{Include: regexp.MustCompile(".*")}
	albums, err := SelectAlbumsForSharing(context.Background(), c, opts)
	if err != nil {
		t.Fatalf("SelectAlbumsForSharing: %v", err)
	}

	t.Logf("server returned %d matching album(s)", len(albums))
	for _, a := range albums {
		if a.AlbumName == "" {
			t.Errorf("album %s has empty AlbumName", a.Id)
		}
	}

	// ResolveUser against the authenticated user's own name/email is a
	// read-only lookup too — it must not modify anything.
	me, err := c.API.GetMyUserWithResponse(context.Background())
	if err != nil {
		t.Fatalf("GetMyUser: %v", err)
	}
	if me.JSON200 == nil {
		t.Fatal("GetMyUser: response had no body")
	}

	user, err := ResolveUser(context.Background(), c, string(me.JSON200.Email))
	if err != nil {
		t.Fatalf("ResolveUser(%q): %v", me.JSON200.Email, err)
	}
	if user.Id != me.JSON200.Id {
		t.Errorf("ResolveUser returned user %s, want %s", user.Id, me.JSON200.Id)
	}
}
