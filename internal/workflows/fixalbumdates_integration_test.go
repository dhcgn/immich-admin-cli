//go:build integration

// This file holds a read-only integration test that talks to a real Immich
// server configured in config.prod.yaml. It is excluded from the normal test
// build (and CI) by the `integration` build tag; run it explicitly with:
//
//	go test -tags integration ./internal/workflows/ -run FixAlbumDates
//
// It only reads (CheckAlbumDates) and never calls FixAlbumDates.
package workflows

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/config"
)

func TestCheckAlbumDatesIntegration(t *testing.T) {
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

	checks, err := CheckAlbumDates(context.Background(), c, FixAlbumDatesOptions{OffsetDays: DefaultOffsetDays})
	if err != nil {
		t.Fatalf("CheckAlbumDates: %v", err)
	}

	t.Logf("server returned %d date-pattern album(s)", len(checks))
	for _, check := range checks {
		if check.Album.AlbumName == "" {
			t.Errorf("album %s has empty AlbumName", check.Album.Id)
		}
		if len(check.Mismatches) > 0 {
			t.Logf("  %s (%s): %d mismatch(es)", check.Album.AlbumName, check.Pattern.Kind, len(check.Mismatches))
		}
	}
}
