package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// albumsRenameCommand exposes PATCH /albums/{id} (updateAlbumInfo) for renames.
func albumsRenameCommand() *cli.Command {
	return &cli.Command{
		Name:  "rename",
		Usage: "Rename an album (PATCH /albums/{id})",
		Description: "Renames exactly one album to --name. Target it with exactly one of " +
			"--album-id or --album-name (exact match, must resolve to a single album).",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Usage: "new album `NAME`", Required: true},
			&cli.StringFlag{Name: "album-id", Usage: "target album `UUID` (mutually exclusive with --album-name)"},
			&cli.StringFlag{Name: "album-name", Usage: "target album's current name, exact match (mutually exclusive with --album-id)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print the planned rename without changing anything"},
		},
		Action: albumsRename,
	}
}

func albumsRename(ctx context.Context, cmd *cli.Command) error {
	newName := cmd.String("name")
	if err := validateAlbumRenameFlags(newName, cmd.String("album-id"), cmd.String("album-name")); err != nil {
		return err
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	var album immichapi.AlbumResponseDto
	if idStr := cmd.String("album-id"); idStr != "" {
		parsed, err := uuid.Parse(idStr)
		if err != nil {
			return fmt.Errorf("invalid --album-id %q: %w", idStr, err)
		}
		uid := openapi_types.UUID(parsed)
		album, err = workflows.ResolveAlbum(ctx, c, &uid, "")
		if err != nil {
			return err
		}
	} else {
		// No --yes flag here: a whitespace-variant match always asks.
		album, err = resolveAlbumByName(ctx, c, os.Stdin, os.Stdout, cmd.String("album-name"), false)
		if err != nil {
			return err
		}
	}

	if cmd.Bool("dry-run") {
		fmt.Printf("[dry-run] would rename album %s %q -> %q\n", album.Id, album.AlbumName, newName)
		return nil
	}

	body := immichapi.UpdateAlbumDto{AlbumName: &newName}
	resp, err := c.API.UpdateAlbumInfoWithResponse(ctx, album.Id, body)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("renaming album: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Renamed album %s %q -> %q\n", album.Id, album.AlbumName, newName)
	return nil
}

// validateAlbumRenameFlags is pure so it is directly unit-testable.
func validateAlbumRenameFlags(newName, albumIDStr, albumName string) error {
	if strings.TrimSpace(newName) == "" {
		return fmt.Errorf("--name is required: new album name must not be empty")
	}
	return validateDownloadAlbumAlbumFlags(albumIDStr, albumName)
}
