//go:build integration

// Staging integration test for the server-version gate: it verifies that the
// staging server actually meets the minimum version this CLI needs
// (client.MinServer*, warn-only at runtime). A failure here means staging
// must be upgraded, not that the CLI is broken.
//
// Run explicitly with:
//
//	go test -tags integration ./internal/commands/ -run ServerVersion
package commands

import (
	"context"
	"testing"

	"github.com/dhcgn/immich-admin-cli/internal/client"
)

func TestServerVersionIntegration(t *testing.T) {
	c := stagingTestClient(t)

	v, err := c.ServerVersion(context.Background())
	if err != nil {
		t.Fatalf("ServerVersion: %v", err)
	}
	t.Logf("staging server version: %s (minimum: %s)",
		client.FormatServerVersion(v), client.MinServerVersionString())

	if client.ServerTooOld(v) {
		t.Errorf("staging server %s is below the minimum %s; upgrade staging",
			client.FormatServerVersion(v), client.MinServerVersionString())
	}

	// The runtime gate must agree with the pure comparator.
	if err := c.CheckServerVersion(context.Background()); err != nil {
		t.Errorf("CheckServerVersion: %v", err)
	}
}
