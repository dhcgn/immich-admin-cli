package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// tagsBulkTagCommand exposes PUT /tags/assets (bulkTagAssets).
func tagsBulkTagCommand() *cli.Command {
	return &cli.Command{
		Name:      "bulk-tag",
		Usage:     "Add multiple tags to multiple assets in one request (PUT /tags/assets)",
		ArgsUsage: "[ASSET_ID ...]",
		Flags: []cli.Flag{
			idsFileFlag(),
			&cli.StringSliceFlag{Name: "tag-id", Usage: "tag `ID` to add (repeatable)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print what would be tagged without changing anything"},
		},
		Action: tagsBulkTag,
	}
}

// tagsTagCommand exposes PUT /tags/{id}/assets (tagAssets).
func tagsTagCommand() *cli.Command {
	return &cli.Command{
		Name:      "tag",
		Usage:     "Add one tag to multiple assets (PUT /tags/{id}/assets)",
		ArgsUsage: "TAG_ID [ASSET_ID ...]",
		Flags: []cli.Flag{
			idsFileFlag(),
			&cli.BoolFlag{Name: "dry-run", Usage: "print what would be tagged without changing anything"},
		},
		Action: tagsTag,
	}
}

func tagsBulkTag(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	tagIDs, err := parseTagIDs(cmd.StringSlice("tag-id"))
	if err != nil {
		return err
	}
	if len(tagIDs) == 0 {
		return fmt.Errorf("no tags given: pass at least one --tag-id")
	}

	if cmd.Bool("dry-run") {
		for _, id := range ids {
			for _, tagID := range tagIDs {
				fmt.Printf("[dry-run] would tag asset %s with tag %s\n", id, tagID)
			}
		}
		fmt.Printf("[dry-run] %d asset(s) x %d tag(s)\n", len(ids), len(tagIDs))
		return nil
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	resp, err := c.API.BulkTagAssetsWithResponse(ctx, immichapi.TagBulkAssetsDto{AssetIds: ids, TagIds: tagIDs})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("tagging assets: %w", err)
	}
	fmt.Printf("Tagged %d asset(s) with %d tag(s)\n", len(ids), len(tagIDs))
	return nil
}

func tagsTag(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return fmt.Errorf("no tag ID given: pass TAG_ID as the first argument")
	}
	tagID, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid TAG_ID %q: %w", args[0], err)
	}

	// Asset IDs come from the remaining positionals and/or --ids-file.
	// collectIDs reads cmd.Args() directly, so scope it to the tail.
	assetIDs, err := collectIDsFrom(cmd, args[1:])
	if err != nil {
		return err
	}

	if cmd.Bool("dry-run") {
		for _, id := range assetIDs {
			fmt.Printf("[dry-run] would tag asset %s with tag %s\n", id, tagID)
		}
		fmt.Printf("[dry-run] %d asset(s) would be tagged\n", len(assetIDs))
		return nil
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	uid := openapi_types.UUID(tagID)
	resp, err := c.API.TagAssetsWithResponse(ctx, uid, immichapi.BulkIdsDto{Ids: assetIDs})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("tagging assets: %w", err)
	}
	failures := 0
	if resp.JSON200 != nil {
		for _, r := range *resp.JSON200 {
			if r.Success {
				fmt.Printf("%s: tagged\n", r.Id)
			} else {
				fmt.Fprintf(os.Stderr, "Error: asset %s: %v\n", r.Id, bulkIDError(r))
				failures++
			}
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d assets failed", failures, len(assetIDs))
	}
	return nil
}

// parseTagIDs parses --tag-id values into UUIDs.
func parseTagIDs(raw []string) ([]openapi_types.UUID, error) {
	ids := make([]openapi_types.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid --tag-id %q: %w", s, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// collectIDsFrom gathers UUIDs from explicit args plus --ids-file (shared
// comment/blank-line handling). At least one ID must result.
func collectIDsFrom(cmd *cli.Command, args []string) ([]openapi_types.UUID, error) {
	raw := append([]string{}, args...)
	if path := cmd.String("ids-file"); path != "" {
		fileIDs, err := readIDLines(path)
		if err != nil {
			return nil, err
		}
		raw = append(raw, fileIDs...)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("no asset IDs given: pass them as arguments or via --ids-file")
	}
	ids := make([]openapi_types.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid asset ID %q: %w", s, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// bulkIDError renders a BulkIdResponseDto failure reason.
func bulkIDError(r immichapi.BulkIdResponseDto) string {
	if r.ErrorMessage != nil && *r.ErrorMessage != "" {
		return *r.ErrorMessage
	}
	if r.Error != nil && *r.Error != "" {
		return string(*r.Error)
	}
	return "server reported failure"
}
