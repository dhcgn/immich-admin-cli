package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"

	"github.com/google/uuid"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// Albums returns the `albums` command group (spec tag: Albums).
func Albums() *cli.Command {
	return &cli.Command{
		Name:  "albums",
		Usage: "Album operations",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List albums (GET /albums)",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "asset-id",
						Usage: "filter albums containing this asset `ID` (ignores other filters)",
					},
					&cli.StringFlag{
						Name:  "id",
						Usage: "filter by album `ID`",
					},
					&cli.StringFlag{
						Name:  "name",
						Usage: "filter by album name (exact match)",
					},
					&cli.StringFlag{
						Name:  "owned",
						Usage: "filter by ownership: true = only owned, false = only shared-with-me",
					},
					&cli.StringFlag{
						Name:  "shared",
						Usage: "filter by shared status: true = only shared, false = not shared",
					},
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the raw response as a JSON array",
					},
				},
				Action: albumsList,
			},
			{
				Name:      "get",
				Usage:     "Show one or more albums by ID, including members (GET /albums/{id})",
				ArgsUsage: "[ALBUM_ID ...]",
				Flags: []cli.Flag{
					idsFileFlag(),
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the raw responses as a JSON array",
					},
				},
				Action: albumsGet,
			},
		},
	}
}

func albumsList(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	params, err := buildGetAllAlbumsParams(cmd)
	if err != nil {
		return err
	}

	resp, err := c.API.GetAllAlbumsWithResponse(ctx, params)
	if err != nil {
		return fmt.Errorf("calling GET /albums: %w", err)
	}
	if err := client.Check(resp, http.StatusOK); err != nil {
		return fmt.Errorf("GET /albums: %w", err)
	}

	albums := []immichapi.AlbumResponseDto{}
	if resp.JSON200 != nil {
		albums = *resp.JSON200
	}
	sort.Slice(albums, func(i, j int) bool { return albums[i].AlbumName < albums[j].AlbumName })

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(albums)
	}

	for _, a := range albums {
		printAlbumLine(a)
	}
	fmt.Printf("%d album(s)\n", len(albums))
	return nil
}

func albumsGet(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Fan out over the single-album endpoint: continue on per-ID errors and
	// report failures at the end.
	var albums []*immichapi.AlbumResponseDto
	failures := 0
	for _, id := range ids {
		resp, err := c.API.GetAlbumInfoWithResponse(ctx, id, &immichapi.GetAlbumInfoParams{})
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: album %s: %v\n", id, err)
			failures++
			continue
		}
		albums = append(albums, resp.JSON200)
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(albums); err != nil {
			return err
		}
	} else {
		for i, a := range albums {
			if i > 0 {
				fmt.Println()
			}
			printAlbum(a)
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d albums failed", failures, len(ids))
	}
	return nil
}

// buildGetAllAlbumsParams maps CLI flags to GetAllAlbumsParams.
func buildGetAllAlbumsParams(cmd *cli.Command) (*immichapi.GetAllAlbumsParams, error) {
	params := &immichapi.GetAllAlbumsParams{}

	if v := cmd.String("asset-id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("invalid --asset-id %q: %w", v, err)
		}
		params.AssetId = &id
	}
	if v := cmd.String("id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("invalid --id %q: %w", v, err)
		}
		params.Id = &id
	}
	if v := cmd.String("name"); v != "" {
		params.Name = &v
	}
	if err := setBoolFlag(cmd, "owned", func(b bool) { params.IsOwned = &b }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "shared", func(b bool) { params.IsShared = &b }); err != nil {
		return nil, err
	}

	return params, nil
}

// printAlbumLine prints a compact one-line summary of an album (used by `albums list`).
func printAlbumLine(a immichapi.AlbumResponseDto) {
	shared := ""
	if a.Shared {
		shared = "  shared"
	}
	fmt.Printf("%s  %s  %d asset(s)%s\n", a.Id, a.AlbumName, a.AssetCount, shared)
}

// printAlbum prints a multi-line detail view of an album including its
// members (used by `albums get`).
func printAlbum(a *immichapi.AlbumResponseDto) {
	fmt.Printf("ID:      %s\n", a.Id)
	fmt.Printf("Name:    %s\n", a.AlbumName)
	fmt.Printf("Assets:  %d\n", a.AssetCount)
	fmt.Printf("Shared:  %t\n", a.Shared)
	fmt.Printf("Created: %s\n", a.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated: %s\n", a.UpdatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Members:\n")
	for _, au := range a.AlbumUsers {
		fmt.Printf("  %s  %s  <%s>  %s\n", au.User.Id, au.User.Name, au.User.Email, au.Role)
	}
}
