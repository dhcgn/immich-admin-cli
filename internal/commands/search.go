package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// Search returns the `search` command group (spec tag: Search).
func Search() *cli.Command {
	return &cli.Command{
		Name:  "search",
		Usage: "Search operations",
		Commands: []*cli.Command{
			{
				Name:  "metadata",
				Usage: "Search assets by metadata criteria (POST /search/metadata)",
				Description: "Search for assets using metadata filters such as file name, type, camera, location, and date ranges. " +
					"Results are paginated; use --all to fetch every page automatically.",
				Flags: []cli.Flag{
					// --- file / name ---
					&cli.StringFlag{
						Name:    "original-file-name",
						Aliases: []string{"n"},
						Usage:   "filter by original file name (substring match)",
					},
					&cli.StringFlag{
						Name:  "original-path",
						Usage: "filter by original file path",
					},
					// --- asset type ---
					&cli.StringFlag{
						Name:  "type",
						Usage: "filter by asset type: IMAGE, VIDEO, AUDIO, OTHER",
					},
					// --- boolean filters ---
					&cli.StringFlag{
						Name:  "is-favorite",
						Usage: "filter favorites: true or false",
					},
					&cli.StringFlag{
						Name:  "is-not-in-album",
						Usage: "filter assets not in any album: true or false",
					},
					&cli.StringFlag{
						Name:  "is-offline",
						Usage: "filter offline assets: true or false",
					},
					&cli.StringFlag{
						Name:  "is-motion",
						Usage: "filter motion photos: true or false",
					},
					// --- location ---
					&cli.StringFlag{
						Name:  "city",
						Usage: "filter by city name",
					},
					&cli.StringFlag{
						Name:  "country",
						Usage: "filter by country name",
					},
					&cli.StringFlag{
						Name:  "state",
						Usage: "filter by state/province name",
					},
					// --- camera ---
					&cli.StringFlag{
						Name:  "make",
						Usage: "filter by camera make",
					},
					&cli.StringFlag{
						Name:  "model",
						Usage: "filter by camera model",
					},
					&cli.StringFlag{
						Name:  "lens-model",
						Usage: "filter by lens model",
					},
					// --- dates ---
					&cli.StringFlag{
						Name:  "taken-after",
						Usage: "filter by taken date after (RFC3339, e.g. 2024-01-01T00:00:00Z)",
					},
					&cli.StringFlag{
						Name:  "taken-before",
						Usage: "filter by taken date before (RFC3339, e.g. 2024-12-31T23:59:59Z)",
					},
					// --- OCR / description ---
					&cli.StringFlag{
						Name:  "ocr",
						Usage: "filter by OCR text content",
					},
					&cli.StringFlag{
						Name:  "description",
						Usage: "filter by description text",
					},
					// --- order ---
					&cli.StringFlag{
						Name:  "order",
						Usage: "sort order: asc or desc (default: desc)",
						Value: "desc",
					},
					// --- pagination ---
					&cli.IntFlag{
						Name:  "page-size",
						Usage: "number of results per page (max 1000)",
						Value: 100,
					},
					&cli.BoolFlag{
						Name:  "all",
						Usage: "fetch all pages automatically (overrides --page-size limit)",
					},
					// --- output format ---
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print raw JSON response",
					},
					&cli.BoolFlag{
						Name:    "ids-only",
						Aliases: []string{"q"},
						Usage:   "print only asset IDs, one per line (useful for piping to other commands)",
					},
				},
				Action: searchMetadata,
			},
		},
	}
}

func searchMetadata(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(cmd)
	if err != nil {
		return err
	}

	body, err := buildMetadataSearchDto(cmd)
	if err != nil {
		return err
	}

	fetchAll := cmd.Bool("all")
	jsonMode := cmd.Bool("json")
	idsOnly := cmd.Bool("ids-only")

	page := 1
	totalPrinted := 0

	for {
		body.Page = &page

		resp, err := c.API.SearchAssetsWithResponse(ctx, &immichapi.SearchAssetsParams{}, *body)
		if err != nil {
			return fmt.Errorf("calling POST /search/metadata: %w", err)
		}
		if err := client.Check(resp, http.StatusOK); err != nil {
			return fmt.Errorf("POST /search/metadata: %w", err)
		}

		assets := resp.JSON200.Assets

		switch {
		case jsonMode:
			raw, err := json.MarshalIndent(assets.Items, "", "  ")
			if err != nil {
				return fmt.Errorf("marshalling response: %w", err)
			}
			fmt.Println(string(raw))

		case idsOnly:
			for _, a := range assets.Items {
				fmt.Println(a.Id)
			}

		default:
			for _, a := range assets.Items {
				fmt.Printf("%s\t%s\t%s\n", a.Id, a.OriginalFileName, a.Type)
			}
		}

		totalPrinted += len(assets.Items)

		// Pagination: nextPage is the next page token; null means we're done.
		if !fetchAll || assets.NextPage == nil || *assets.NextPage == "" {
			if !jsonMode && !idsOnly {
				fmt.Fprintf(cmd.Root().Writer, "--- %d asset(s) found (page %d) ---\n", totalPrinted, page)
			}
			break
		}

		// Parse the next page number from the token (Immich returns it as a numeric string).
		nextPage, err := strconv.Atoi(*assets.NextPage)
		if err != nil {
			// If it's not a plain integer token, stop — we can't continue safely.
			break
		}
		page = nextPage
	}

	return nil
}

// buildMetadataSearchDto maps CLI flags to the API request body.
func buildMetadataSearchDto(cmd *cli.Command) (*immichapi.SearchAssetsJSONRequestBody, error) {
	body := &immichapi.MetadataSearchDto{}

	if v := cmd.String("original-file-name"); v != "" {
		body.OriginalFileName = &v
	}
	if v := cmd.String("original-path"); v != "" {
		body.OriginalPath = &v
	}
	if v := cmd.String("type"); v != "" {
		t := immichapi.AssetTypeEnum(v)
		body.Type = &t
	}
	if v := cmd.String("city"); v != "" {
		body.City = &v
	}
	if v := cmd.String("country"); v != "" {
		body.Country = &v
	}
	if v := cmd.String("state"); v != "" {
		body.State = &v
	}
	if v := cmd.String("make"); v != "" {
		body.Make = &v
	}
	if v := cmd.String("model"); v != "" {
		body.Model = &v
	}
	if v := cmd.String("lens-model"); v != "" {
		body.LensModel = &v
	}
	if v := cmd.String("ocr"); v != "" {
		body.Ocr = &v
	}
	if v := cmd.String("description"); v != "" {
		body.Description = &v
	}
	if v := cmd.String("order"); v != "" {
		o := immichapi.AssetOrder(v)
		body.Order = &o
	}
	if err := setBoolFlag(cmd, "is-favorite", func(b bool) { body.IsFavorite = &b }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "is-not-in-album", func(b bool) { body.IsNotInAlbum = &b }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "is-offline", func(b bool) { body.IsOffline = &b }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "is-motion", func(b bool) { body.IsMotion = &b }); err != nil {
		return nil, err
	}

	size := cmd.Int("page-size")
	body.Size = &size

	return body, nil
}

// setBoolFlag parses an optional "true"/"false" string flag and calls set if provided.
func setBoolFlag(cmd *cli.Command, name string, set func(bool)) error {
	v := cmd.String(name)
	if v == "" {
		return nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fmt.Errorf("flag --%s: expected true or false, got %q", name, v)
	}
	set(b)
	return nil
}
