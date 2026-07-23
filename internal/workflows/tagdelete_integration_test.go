//go:build integration

// This file holds a read-only integration test that talks to a real Immich
// server configured in config.prod.yaml. It is excluded from the normal test
// build (and CI) by the `integration` build tag; run it explicitly with:
//
//	go test -tags integration ./internal/workflows/ -run TagDelete
//
// It only reads (SelectTagsForDeletion) and never deletes anything.
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

// configPath points at the repo-root config.prod.yaml relative to this package.
const configPath = "../../config.prod.yaml"

func TestTagDeleteSelectIntegration(t *testing.T) {
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

	// Match-all include; this is a read-only call — it must not delete.
	opts := TagDeleteOptions{Include: regexp.MustCompile(".*")}
	tags, err := SelectTagsForDeletion(context.Background(), c, opts)
	if err != nil {
		t.Fatalf("SelectTagsForDeletion: %v", err)
	}

	t.Logf("server returned %d matching tag(s)", len(tags))
	for _, tag := range tags {
		if tag.Value == "" {
			t.Errorf("tag %s has empty Value", tag.Id)
		}
	}
}
