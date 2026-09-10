package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// ImmichWorkflow returns the `immich-workflow` command group (spec tag:
// Workflows): thin wrappers over Immich's own server-side workflows API
// (listing workflows and reading their run logs). Not to be confused with
// `client-workflow` (alias `cw`), the client-side multi-step orchestrations
// that are this tool's main purpose. The plural `workflows` alone is
// deliberately not used as a group name to keep the two concepts apart.
func ImmichWorkflow() *cli.Command {
	return &cli.Command{
		Name:  "immich-workflow",
		Usage: "Server-side workflow operations",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List server workflows (GET /workflows)",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "description", Usage: "filter by workflow description"},
					&cli.StringFlag{Name: "enabled", Usage: "filter by enabled status: true or false"},
					&cli.StringFlag{Name: "id", Usage: "filter by workflow `ID`"},
					&cli.StringFlag{Name: "logging", Usage: "filter by whether the workflow logs run results: true or false"},
					&cli.StringFlag{Name: "name", Usage: "filter by workflow name"},
					&cli.StringFlag{Name: "trigger", Usage: "filter by trigger type: AssetCreate, AssetMetadataExtraction, or AssetTagged"},
					&cli.BoolFlag{Name: "json", Usage: "print the raw response as a JSON array"},
				},
				Action: immichWorkflowsList,
			},
			{
				Name:      "get",
				Usage:     "Show one or more server workflows by ID (GET /workflows/{id})",
				ArgsUsage: "[WORKFLOW_ID ...]",
				Flags: []cli.Flag{
					idsFileFlag(),
					&cli.BoolFlag{Name: "json", Usage: "print the raw responses as a JSON array"},
				},
				Action: immichWorkflowsGet,
			},
			{
				Name:      "logs",
				Usage:     "Show run logs for one server workflow (GET /workflows/{id}/logs)",
				ArgsUsage: "WORKFLOW_ID",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "before", Usage: "only show runs before this date/time (RFC3339)"},
					&cli.IntFlag{Name: "limit", Usage: "maximum number of log entries (default: server default, 50)"},
					&cli.StringFlag{Name: "result", Usage: "only show runs with this result: completed, halted, or error"},
					&cli.BoolFlag{Name: "json", Usage: "print the raw response as a JSON array"},
				},
				Action: immichWorkflowLogs,
			},
		},
	}
}

func immichWorkflowsList(ctx context.Context, cmd *cli.Command) error {
	params, err := buildSearchWorkflowsParams(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	resp, err := c.API.SearchWorkflowsWithResponse(ctx, params)
	if err != nil {
		return fmt.Errorf("calling GET /workflows: %w", err)
	}
	if err := client.Check(resp, http.StatusOK); err != nil {
		return fmt.Errorf("GET /workflows: %w", err)
	}

	workflows := []immichapi.WorkflowResponseDto{}
	if resp.JSON200 != nil {
		workflows = *resp.JSON200
	}
	sort.Slice(workflows, func(i, j int) bool { return workflowName(workflows[i]) < workflowName(workflows[j]) })

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(workflows)
	}

	for _, w := range workflows {
		printWorkflowLine(w)
	}
	fmt.Printf("%d workflow(s)\n", len(workflows))
	return nil
}

func immichWorkflowsGet(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Fan out over the single-workflow endpoint: continue on per-ID errors
	// and report failures at the end.
	var workflows []*immichapi.WorkflowResponseDto
	failures := 0
	for _, id := range ids {
		resp, err := c.API.GetWorkflowWithResponse(ctx, id)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: workflow %s: %v\n", id, err)
			failures++
			continue
		}
		workflows = append(workflows, resp.JSON200)
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(workflows); err != nil {
			return err
		}
	} else {
		for i, w := range workflows {
			if i > 0 {
				fmt.Println()
			}
			printWorkflow(w)
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d workflows failed", failures, len(ids))
	}
	return nil
}

func immichWorkflowLogs(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 1 {
		return fmt.Errorf("expected exactly 1 positional argument (WORKFLOW_ID), got %d", len(args))
	}
	id, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid WORKFLOW_ID %q: %w", args[0], err)
	}

	params, err := buildGetWorkflowLogsParams(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	uid := openapi_types.UUID(id)
	resp, err := c.API.GetWorkflowLogsWithResponse(ctx, uid, params)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("GET /workflows/{id}/logs: %w", err)
	}
	entries := []immichapi.WorkflowLogEntryDto{}
	if resp.JSON200 != nil {
		entries = *resp.JSON200
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	for _, e := range entries {
		printWorkflowLogLine(e)
	}
	fmt.Printf("%d log entr(y/ies)\n", len(entries))
	return nil
}

// buildSearchWorkflowsParams maps CLI flags to SearchWorkflowsParams.
// Every field is optional.
func buildSearchWorkflowsParams(cmd *cli.Command) (*immichapi.SearchWorkflowsParams, error) {
	params := &immichapi.SearchWorkflowsParams{}

	if v := cmd.String("description"); v != "" {
		params.Description = &v
	}
	if v := cmd.String("name"); v != "" {
		params.Name = &v
	}
	if v := cmd.String("id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("invalid --id %q: %w", v, err)
		}
		uid := openapi_types.UUID(id)
		params.Id = &uid
	}
	if v := cmd.String("trigger"); v != "" {
		t, err := resolveWorkflowTrigger(v)
		if err != nil {
			return nil, err
		}
		params.Trigger = &t
	}
	if err := setBoolFlag(cmd, "enabled", func(b bool) { params.Enabled = &b }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "logging", func(b bool) { params.Logging = &b }); err != nil {
		return nil, err
	}
	return params, nil
}

// buildGetWorkflowLogsParams maps CLI flags to GetWorkflowLogsParams. Every
// field is optional (nil leaves the server default, e.g. limit 50).
func buildGetWorkflowLogsParams(cmd *cli.Command) (*immichapi.GetWorkflowLogsParams, error) {
	params := &immichapi.GetWorkflowLogsParams{}

	if v := cmd.String("before"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, fmt.Errorf("invalid --before %q: %w (expected RFC3339)", v, err)
		}
		params.Before = &t
	}
	if cmd.IsSet("limit") {
		limit := int(cmd.Int("limit"))
		if limit < 0 {
			return nil, fmt.Errorf("invalid --limit %d: must not be negative", limit)
		}
		params.Limit = &limit
	}
	if v := cmd.String("result"); v != "" {
		r, err := resolveWorkflowResult(v)
		if err != nil {
			return nil, err
		}
		params.Result = &r
	}
	return params, nil
}

// resolveWorkflowTrigger validates --trigger against the spec enum.
func resolveWorkflowTrigger(raw string) (immichapi.WorkflowTrigger, error) {
	switch t := immichapi.WorkflowTrigger(raw); t {
	case immichapi.AssetCreate, immichapi.AssetMetadataExtraction, immichapi.AssetTagged:
		return t, nil
	default:
		return "", fmt.Errorf("invalid --trigger %q: must be one of AssetCreate, AssetMetadataExtraction, AssetTagged", raw)
	}
}

// resolveWorkflowResult validates --result against the spec enum.
func resolveWorkflowResult(raw string) (immichapi.WorkflowResult, error) {
	switch r := immichapi.WorkflowResult(raw); r {
	case immichapi.WorkflowResultCompleted, immichapi.WorkflowResultHalted, immichapi.WorkflowResultError:
		return r, nil
	default:
		return "", fmt.Errorf("invalid --result %q: must be one of completed, halted, error", raw)
	}
}

// workflowName returns the printable workflow name (empty when unset).
func workflowName(w immichapi.WorkflowResponseDto) string {
	if w.Name == nil {
		return ""
	}
	return *w.Name
}

func printWorkflowLine(w immichapi.WorkflowResponseDto) {
	fmt.Printf("%s  %s  enabled=%t logging=%t  %s\n", w.Id, workflowName(w), w.Enabled, w.Logging, w.Trigger)
}

func printWorkflow(w *immichapi.WorkflowResponseDto) {
	fmt.Printf("ID:       %s\n", w.Id)
	fmt.Printf("Name:     %s\n", workflowName(*w))
	if w.Description != nil && *w.Description != "" {
		fmt.Printf("Descr:    %s\n", *w.Description)
	}
	fmt.Printf("Enabled:  %t\n", w.Enabled)
	fmt.Printf("Logging:  %t\n", w.Logging)
	fmt.Printf("Trigger:  %s\n", w.Trigger)
	fmt.Printf("Steps:    %d\n", len(w.Steps))
	fmt.Printf("Created:  %s\n", w.CreatedAt)
	fmt.Printf("Updated:  %s\n", w.UpdatedAt)
}

func printWorkflowLogLine(e immichapi.WorkflowLogEntryDto) {
	line := fmt.Sprintf("%s  %s  %s", e.At.Format("2006-01-02 15:04:05"), e.Result, e.Id)
	if e.LastStep != nil {
		line += fmt.Sprintf("  last-step=%s#%d", e.LastStep.Method, e.LastStep.Index)
	}
	if e.TriggerDataId != nil {
		line += fmt.Sprintf("  trigger-data=%s", *e.TriggerDataId)
	}
	fmt.Println(line)
}
