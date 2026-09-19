package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// albumsCreateCommand exposes POST /albums (createAlbum).
func albumsCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create an album (POST /albums)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Usage: "new album `NAME`", Required: true},
			&cli.StringFlag{Name: "description", Usage: "album `DESCRIPTION`"},
		},
		Action: albumsCreate,
	}
}

func albumsCreate(ctx context.Context, cmd *cli.Command) error {
	name := strings.TrimSpace(cmd.String("name"))
	if name == "" {
		return fmt.Errorf("--name is required: new album name must not be empty")
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	body := immichapi.CreateAlbumDto{AlbumName: name}
	if desc := cmd.String("description"); desc != "" {
		body.Description = &desc
	}
	resp, err := c.API.CreateAlbumWithResponse(ctx, body)
	if err == nil {
		err = client.Check(resp, http.StatusCreated)
	}
	if err != nil {
		return fmt.Errorf("creating album: %w", err)
	}
	if resp.JSON201 == nil {
		return fmt.Errorf("creating album: response had no body")
	}
	printAlbumLine(*resp.JSON201)
	fmt.Fprintf(os.Stderr, "Created album %s %q\n", resp.JSON201.Id, resp.JSON201.AlbumName)
	return nil
}
