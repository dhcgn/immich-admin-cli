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
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// albumsAddUsersCommand exposes PUT /albums/{id}/users (addUsersToAlbum).
func albumsAddUsersCommand() *cli.Command {
	return &cli.Command{
		Name:      "add-users",
		Usage:     "Share an album with a user (PUT /albums/{id}/users)",
		ArgsUsage: "ALBUM_ID",
		Description: "Shares one album with one user at --role. --user accepts an exact user UUID " +
			"or a case-insensitive name/email substring (resolved to exactly one user, same as " +
			"client-workflow add-users-to-album-with-pattern). Repeat the command for multiple users.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "user", Usage: "target user: exact user `UUID`, or a case-insensitive substring of their name/email", Required: true},
			&cli.StringFlag{Name: "role", Usage: "album role to grant: editor, viewer, or owner", Value: string(immichapi.AlbumUserRoleViewer)},
			&cli.BoolFlag{Name: "dry-run", Usage: "print the planned share without changing anything"},
		},
		Action: albumsAddUsers,
	}
}

func albumsAddUsers(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 1 {
		return fmt.Errorf("expected exactly 1 positional argument (ALBUM_ID), got %d", len(args))
	}
	albumID, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid ALBUM_ID %q: %w", args[0], err)
	}
	role, err := resolveAlbumUserRole(cmd.String("role"))
	if err != nil {
		return err
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	user, err := workflows.ResolveUser(ctx, c, cmd.String("user"))
	if err != nil {
		return err
	}

	if cmd.Bool("dry-run") {
		fmt.Printf("[dry-run] would share album %s with %s <%s> as %s\n", albumID, user.Name, user.Email, role)
		return nil
	}

	uid := openapi_types.UUID(albumID)
	body := immichapi.AddUsersDto{
		AlbumUsers: []immichapi.AlbumUserAddDto{{UserId: user.Id, Role: &role}},
	}
	resp, err := c.API.AddUsersToAlbumWithResponse(ctx, uid, body)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("sharing album: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Shared album %s with %s <%s> as %s\n", albumID, user.Name, user.Email, role)
	return nil
}

// resolveAlbumUserRole validates --role against the spec enum.
func resolveAlbumUserRole(raw string) (immichapi.AlbumUserRole, error) {
	switch role := immichapi.AlbumUserRole(raw); role {
	case immichapi.AlbumUserRoleEditor, immichapi.AlbumUserRoleViewer, immichapi.AlbumUserRoleOwner:
		return role, nil
	default:
		return "", fmt.Errorf("invalid --role %q: must be editor, viewer, or owner", raw)
	}
}
