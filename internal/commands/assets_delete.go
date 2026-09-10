package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// assetsDeleteCommand exposes DELETE /assets (deleteAssets).
func assetsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete one or more assets (DELETE /assets)",
		ArgsUsage: "[ASSET_ID ...]",
		Flags: []cli.Flag{
			idsFileFlag(),
			&cli.BoolFlag{Name: "force", Usage: "permanently delete instead of moving to trash"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print the assets that would be deleted without changing anything"},
			&cli.BoolFlag{Name: "yes", Usage: "skip the confirmation prompt before deleting assets"},
		},
		Action: assetsDelete,
	}
}

func assetsDelete(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	force := cmd.Bool("force")
	dryRun := cmd.Bool("dry-run")

	if dryRun {
		for _, id := range ids {
			fmt.Printf("[dry-run] would delete asset %s (force=%t)\n", id, force)
		}
		fmt.Printf("[dry-run] %d asset(s) would be deleted\n", len(ids))
		return nil
	}

	if !cmd.Bool("yes") {
		mode := "moved to trash"
		if force {
			mode = "permanently deleted"
		}
		fmt.Printf("This will delete %d asset(s) (%s).\n", len(ids), mode)
		fmt.Print("Proceed? [y/N]: ")
		if !confirm(os.Stdin) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	return workflows.RunBatch(chunkUUIDs(ids, 500),
		func(chunk []openapi_types.UUID) string { return fmt.Sprintf("%d asset(s)", len(chunk)) },
		func(chunk []openapi_types.UUID) error {
			resp, err := c.API.DeleteAssetsWithResponse(ctx, immichapi.AssetBulkDeleteDto{Ids: chunk, Force: &force})
			if err == nil {
				err = client.Check(resp, http.StatusNoContent)
			}
			if err != nil {
				return fmt.Errorf("deleting %d asset(s): %w", len(chunk), err)
			}
			for _, id := range chunk {
				fmt.Printf("%s: deleted\n", id)
			}
			return nil
		},
	)
}

// chunkUUIDs splits ids into chunks of at most n.
func chunkUUIDs(ids []openapi_types.UUID, n int) [][]openapi_types.UUID {
	var chunks [][]openapi_types.UUID
	for len(ids) > 0 {
		if len(ids) < n {
			n = len(ids)
		}
		chunks = append(chunks, ids[:n])
		ids = ids[n:]
	}
	return chunks
}
