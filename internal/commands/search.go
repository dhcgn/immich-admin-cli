package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
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
					"Results are paginated; use --all to fetch every page automatically. Trashed assets are excluded.",
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
						Usage: "filter by OCR text recognized in the image (fuzzy word match, e.g. a year printed on a scan)",
					},
					&cli.StringFlag{
						Name:  "description",
						Usage: "filter by description text (substring match)",
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
	c, err := newClient(ctx, cmd)
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

	var pager workflows.SearchPager
	totalPrinted := 0

	for {
		pager.Apply(body)

		resp, err := workflows.SearchExcludingTrash(ctx, c, *body)
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

		// Pagination: cursor scheme (v3.2+) preferred, legacy page scheme
		// (pre-3.2 servers) as fallback; either reports false when done.
		if !fetchAll || !pager.Next(assets) {
			if !jsonMode && !idsOnly {
				fmt.Fprintf(cmd.Root().Writer, "--- %d asset(s) found (page %d) ---\n", totalPrinted, pager.Pages())
			}
			break
		}
	}

	return nil
}

// buildMetadataSearchDto maps CLI flags to the API request body. It uses only
// the structured shape (filter/orderBy/cursor, v3.2+): the flat fields are
// deprecated and the server rejects mixing the two shapes (400).
func buildMetadataSearchDto(cmd *cli.Command) (*immichapi.SearchAssetsJSONRequestBody, error) {
	f := immichapi.SearchFilter{}
	str := func(name string) *string {
		if v := cmd.String(name); v != "" {
			return &v
		}
		return nil
	}
	// Substring match, same as the legacy flat fields (server: ILIKE '%v%').
	like := func(name string) *immichapi.StringPatternFilter {
		if v := str(name); v != nil {
			return &immichapi.StringPatternFilter{Like: v}
		}
		return nil
	}
	// Exact match, same as the legacy flat fields.
	eq := func(name string) *immichapi.StringFilterNullable {
		if v := str(name); v != nil {
			return &immichapi.StringFilterNullable{Eq: v}
		}
		return nil
	}

	f.OriginalFileName = like("original-file-name")
	f.OriginalPath = like("original-path")
	f.Description = like("description")
	f.City, f.Country, f.State = eq("city"), eq("country"), eq("state")
	f.Make, f.Model, f.LensModel = eq("make"), eq("model"), eq("lens-model")
	if v := str("ocr"); v != nil {
		f.Ocr = &immichapi.StringSimilarityFilter{Matches: *v}
	}
	if v := str("type"); v != nil {
		t := immichapi.AssetTypeEnum(*v)
		if !t.Valid() {
			return nil, fmt.Errorf("invalid --type %q: must be IMAGE, VIDEO, AUDIO, or OTHER", *v)
		}
		f.Type = &immichapi.EnumFilterAssetType{Eq: &t}
	}
	for _, d := range []struct {
		name string
		set  func(t time.Time)
	}{
		{"taken-after", func(t time.Time) { ensureDate(&f.TakenAt).Gte = &t }},
		{"taken-before", func(t time.Time) { ensureDate(&f.TakenAt).Lte = &t }},
	} {
		if v := str(d.name); v != nil {
			t, err := time.Parse(time.RFC3339, *v)
			if err != nil {
				return nil, fmt.Errorf("flag --%s: expected RFC3339 (e.g. 2024-01-01T00:00:00Z), got %q", d.name, *v)
			}
			d.set(t)
		}
	}
	boolFilter := func(b bool) *immichapi.BoolFilter { return &immichapi.BoolFilter{Eq: b} }
	if err := setBoolFlag(cmd, "is-favorite", func(b bool) { f.IsFavorite = boolFilter(b) }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "is-not-in-album", func(b bool) { f.HasAlbums = boolFilter(!b) }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "is-offline", func(b bool) { f.IsOffline = boolFilter(b) }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "is-motion", func(b bool) { f.IsMotion = boolFilter(b) }); err != nil {
		return nil, err
	}

	o := immichapi.AssetOrder(cmd.String("order"))
	if o != immichapi.AssetOrderAsc && o != immichapi.AssetOrderDesc {
		return nil, fmt.Errorf("invalid --order %q: must be asc or desc", o)
	}
	size := int(cmd.Int("page-size"))
	return &immichapi.MetadataSearchDto{
		Filter:  &f,
		OrderBy: &immichapi.SearchOrder{Direction: &o}, // Field nil = server default sort field
		Size:    &size,
	}, nil
}

func ensureDate(p **immichapi.DateFilter) *immichapi.DateFilter {
	if *p == nil {
		*p = &immichapi.DateFilter{}
	}
	return *p
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
