//go:build integration

// Staging integration tests for the `immich-workflow` commands (the
// server-side Workflows API: searchWorkflows, getWorkflow, getWorkflowLogs).
// Like all staging tests they reuse stagingConfigPath and skip when
// config.staging.yaml is absent. Every call here is a read-only GET, so no
// cleanup is needed. get/logs need a workflow to exist on staging — when
// there is none, that test skips instead of failing.
//
// Run explicitly with:
//
//	go test -tags integration ./internal/commands/ -run ImmichWorkflow
package commands

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/config"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func stagingTestClient(t *testing.T) *client.Client {
	t.Helper()
	if _, err := os.Stat(stagingConfigPath); err != nil {
		t.Skipf("skipping: %s not found (%v)", filepath.Clean(stagingConfigPath), err)
	}
	cfg, err := config.Load(stagingConfigPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	return c
}

func TestImmichWorkflowListIntegration(t *testing.T) {
	c := stagingTestClient(t)
	ctx := context.Background()

	resp, err := c.API.SearchWorkflowsWithResponse(ctx, &immichapi.SearchWorkflowsParams{})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("SearchWorkflows: %v", err)
	}
	workflows := []immichapi.WorkflowResponseDto{}
	if resp.JSON200 != nil {
		workflows = *resp.JSON200
	}

	t.Logf("server returned %d workflow(s)", len(workflows))
	for _, w := range workflows {
		if w.Id.String() == "00000000-0000-0000-0000-000000000000" {
			t.Errorf("workflow has zero UUID")
		}
	}
}

func TestImmichWorkflowGetLogsIntegration(t *testing.T) {
	c := stagingTestClient(t)
	ctx := context.Background()

	list, err := c.API.SearchWorkflowsWithResponse(ctx, &immichapi.SearchWorkflowsParams{})
	if err == nil {
		err = client.Check(list, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("SearchWorkflows: %v", err)
	}
	if list.JSON200 == nil || len(*list.JSON200) == 0 {
		t.Skip("skipping: staging has no workflows to read logs from")
	}

	// Prefer a workflow with logging enabled; fall back to the first one.
	target := (*list.JSON200)[0]
	for _, w := range *list.JSON200 {
		if w.Logging {
			target = w
			break
		}
	}
	t.Logf("reading workflow %s (logging=%t)", target.Id, target.Logging)

	got, err := c.API.GetWorkflowWithResponse(ctx, target.Id)
	if err == nil {
		err = client.Check(got, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("GetWorkflow(%s): %v", target.Id, err)
	}
	if got.JSON200 == nil || got.JSON200.Id != target.Id {
		t.Fatalf("GetWorkflow(%s): response mismatch", target.Id)
	}

	limit := 5
	logs, err := c.API.GetWorkflowLogsWithResponse(ctx, target.Id, &immichapi.GetWorkflowLogsParams{Limit: &limit})
	if err == nil {
		err = client.Check(logs, http.StatusOK)
	}
	if err != nil {
		t.Fatalf("GetWorkflowLogs(%s): %v", target.Id, err)
	}
	entries := []immichapi.WorkflowLogEntryDto{}
	if logs.JSON200 != nil {
		entries = *logs.JSON200
	}
	t.Logf("server returned %d log entr(y/ies)", len(entries))
	for _, e := range entries {
		if e.At.IsZero() {
			t.Errorf("log entry %s has zero timestamp", e.Id)
		}
		switch e.Result {
		case immichapi.WorkflowResultCompleted, immichapi.WorkflowResultHalted, immichapi.WorkflowResultError:
		default:
			t.Errorf("log entry %s has unexpected result %q", e.Id, e.Result)
		}
	}
}
