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
)

// albumsDeleteCommand exposes DELETE /albums/{id} (deleteAlbum). The server
// trashes the album and finishes the deletion in a background job, so the
// album can linger briefly after the 204 response.
func albumsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete one or more albums by ID (DELETE /albums/{id})",
		ArgsUsage: "[ALBUM_ID ...]",
		Flags: []cli.Flag{
			idsFileFlag(),
			&cli.BoolFlag{Name: "force", Usage: "also delete albums that still contain assets (otherwise non-empty albums are refused)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print the albums that would be deleted without changing anything"},
			&cli.BoolFlag{Name: "yes", Usage: "skip the confirmation prompt before deleting albums"},
		},
		Action: albumsDelete,
	}
}

func albumsDelete(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	force := cmd.Bool("force")

	if cmd.Bool("dry-run") {
		for _, id := range ids {
			fmt.Printf("[dry-run] would delete album %s (force=%t)\n", id, force)
		}
		fmt.Printf("[dry-run] %d album(s) would be deleted\n", len(ids))
		return nil
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Resolve every album first (fail fast on unknown IDs) and enforce the
	// non-empty guard before anything is deleted.
	counts := make([]int, len(ids))
	for i, id := range ids {
		resp, err := c.API.GetAlbumInfoWithResponse(ctx, id, &immichapi.GetAlbumInfoParams{})
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			return fmt.Errorf("fetching album %s: %w", id, err)
		}
		counts[i] = resp.JSON200.AssetCount
		if counts[i] > 0 && !force {
			return fmt.Errorf("album %s (%q) still contains %d asset(s): pass --force to delete it anyway",
				id, resp.JSON200.AlbumName, counts[i])
		}
	}

	if !cmd.Bool("yes") {
		for i, id := range ids {
			fmt.Printf("  %s  %d asset(s)\n", id, counts[i])
		}
		fmt.Printf("This will delete %d album(s).\n", len(ids))
		fmt.Print("Proceed? [y/N]: ")
		if !confirm(os.Stdin) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	return deleteAlbums(ctx, c, ids)
}

// deleteAlbums deletes each album (DELETE /albums/{id}, 204), continuing on
// per-ID errors and returning a summary error so the exit code reflects
// partial failure.
func deleteAlbums(ctx context.Context, c *client.Client, ids []openapi_types.UUID) error {
	failures := 0
	for _, id := range ids {
		resp, err := c.API.DeleteAlbumWithResponse(ctx, id)
		if err == nil {
			err = client.Check(resp, http.StatusNoContent)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: album %s: %v\n", id, err)
			failures++
			continue
		}
		fmt.Printf("Deleted album %s\n", id)
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d albums failed", failures, len(ids))
	}
	return nil
}
