package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

func tzAuditCommand() *cli.Command {
	return &cli.Command{
		Name:  "tz-audit",
		Usage: "Report the time zone classes of assets (none, utc, offset, iana, dst-mismatch, iana-inconsistent); read-only",
		Description: "Pages through POST /search/metadata with EXIF (trash excluded; narrow with the search filter flags) " +
			"and classifies every asset by exifInfo.timeZone:\n" +
			"  none              no time zone at all (EXIF without offset and no GPS) — fix with tz-set\n" +
			"  utc               plain UTC: EXIF had no offset, so the wall clock was taken as UTC\n" +
			"  offset            fixed offset, e.g. UTC+2\n" +
			"  iana              IANA name, e.g. Europe/Berlin (usually inferred from GPS)\n" +
			"  dst-mismatch      (with --reference-zone) a fixed offset that is the reference zone's other offset " +
			"at that date, e.g. UTC+2 in January for Europe/Berlin: camera clock not switched for DST. " +
			"Combine with --country to avoid false hits abroad.\n" +
			"  iana-inconsistent IANA zone whose offset does not match localDateTime − fileCreatedAt\n" +
			"Default output: count per class plus the top cameras per class, then every distinct " +
			"exifInfo.timeZone value with its asset count ((none) = no zone). --class NAME lists that class's assets; " +
			"with --ids-only it prints only IDs for piping into tz-set --ids-file -. Never changes anything.",
		Flags: append(searchFilterFlags(),
			&cli.StringFlag{Name: "reference-zone", Usage: "IANA `ZONE` (e.g. Europe/Berlin) enabling the dst-mismatch class"},
			&cli.StringFlag{Name: "class", Usage: "list the assets of this class `NAME`: " + strings.Join(workflows.TZClasses, ", ")},
			&cli.IntFlag{Name: "top", Usage: "number of cameras shown per class in the summary", Value: 5},
			&cli.BoolFlag{Name: "ids-only", Aliases: []string{"q"}, Usage: "with --class: print only asset IDs, one per line"},
			&cli.BoolFlag{Name: "json", Usage: "print the summary (and with --class the matching assets) as JSON"},
		),
		Action: clientWorkflowTZAudit,
	}
}

func tzSetCommand() *cli.Command {
	return &cli.Command{
		Name:      "tz-set",
		Usage:     "Give assets without any time zone an IANA zone's offset, keeping the wall-clock time they show",
		ArgsUsage: "[ASSET_ID|ASSET_URL ...]",
		Description: "Selects assets whose exifInfo.timeZone is null — from asset IDs/links (arguments or --ids-file, " +
			"e.g. piped from tz-audit --class none --ids-only) or, without IDs, by searching the library (trash " +
			"excluded; narrow with the search filter flags). Assets that already have a time zone or have no " +
			"dateTimeOriginal are skipped, never changed.\n" +
			"For each asset the offset of --time-zone in effect at its local date/time is computed (DST-aware per " +
			"asset) and PUT /assets/{id} (updateAsset) sets dateTimeOriginal to the unchanged wall-clock time with " +
			"that offset, e.g. 2020-07-15T12:00:00+02:00. The shown time stays, the absolute instant moves.\n" +
			"Immich then rewrites the XMP sidecar and re-extracts metadata, so the stored zone ends up as the " +
			"fixed offset (UTC+2), not the IANA name — Immich keeps IANA names only when it can infer them from GPS.",
		Flags: append(searchFilterFlags(),
			idsFileFlag(),
			&cli.StringFlag{Name: "time-zone", Usage: "IANA `ZONE` to apply, e.g. Europe/Berlin (fixed offsets like UTC+1 are rejected)", Required: true},
			&cli.BoolFlag{Name: "dry-run", Usage: "print the planned changes without calling the API"},
			&cli.BoolFlag{Name: "yes", Usage: "skip the confirmation prompt (required when IDs come from stdin)"},
		),
		Action: clientWorkflowTZSet,
	}
}

// tzSearch runs the shared search filter flags through SearchAssetsWithExif.
func tzSearch(ctx context.Context, cmd *cli.Command, c *client.Client) ([]immichapi.AssetResponseDto, error) {
	f, err := buildSearchFilter(cmd)
	if err != nil {
		return nil, err
	}
	return workflows.SearchAssetsWithExif(ctx, c, immichapi.MetadataSearchDto{Filter: f}, func(n int) {
		fmt.Fprintf(os.Stderr, "fetched %d asset(s)...\n", n)
	})
}

func clientWorkflowTZAudit(ctx context.Context, cmd *cli.Command) error {
	class, idsOnly, asJSON := cmd.String("class"), cmd.Bool("ids-only"), cmd.Bool("json")
	if class != "" && !slices.Contains(workflows.TZClasses, class) {
		return fmt.Errorf("invalid --class %q: must be one of %s", class, strings.Join(workflows.TZClasses, ", "))
	}
	if idsOnly && class == "" {
		return fmt.Errorf("--ids-only needs --class")
	}
	if idsOnly && asJSON {
		return fmt.Errorf("--ids-only and --json are mutually exclusive")
	}
	var ref *time.Location
	if z := cmd.String("reference-zone"); z != "" {
		var err error
		if ref, err = workflows.LoadIANAZone(z); err != nil {
			return fmt.Errorf("--reference-zone: %w", err)
		}
	} else if class == workflows.TZClassDSTMismatch {
		return fmt.Errorf("--class %s needs --reference-zone", workflows.TZClassDSTMismatch)
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	assets, err := tzSearch(ctx, cmd, c)
	if err != nil {
		return err
	}
	return printTZAudit(os.Stdout, assets, ref, class, idsOnly, asJSON, int(cmd.Int("top")))
}

type tzAuditAsset struct {
	ID               string   `json:"id"`
	OriginalFileName string   `json:"originalFileName"`
	TimeZone         *string  `json:"timeZone"`
	LocalDateTime    string   `json:"localDateTime"`
	Camera           string   `json:"camera"`
	Classes          []string `json:"classes"`
}

func tzCamera(a immichapi.AssetResponseDto) string {
	if a.ExifInfo == nil {
		return "(unknown)"
	}
	cam := strings.TrimSpace(deref(a.ExifInfo.Make) + " " + deref(a.ExifInfo.Model))
	if cam == "" {
		return "(unknown)"
	}
	return cam
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// byCountDesc returns m's keys, highest count first, ties alphabetical.
func byCountDesc(m map[string]int) []string {
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, func(a, b string) int {
		if m[a] != m[b] {
			return m[b] - m[a]
		}
		return strings.Compare(a, b)
	})
	return keys
}

// printTZAudit renders the tz-audit report. Pure apart from writing to w.
func printTZAudit(w io.Writer, assets []immichapi.AssetResponseDto, ref *time.Location, class string, idsOnly, asJSON bool, top int) error {
	counts := map[string]int{}
	cams := map[string]map[string]int{}
	zones := map[string]int{} // exifInfo.timeZone value → assets, "(none)" for null
	var listed []tzAuditAsset
	for _, a := range assets {
		zone := "(none)"
		if a.ExifInfo != nil && a.ExifInfo.TimeZone != nil {
			zone = *a.ExifInfo.TimeZone
		}
		zones[zone]++
		classes := workflows.ClassifyTZ(a, ref)
		for _, cl := range classes {
			counts[cl]++
			if cams[cl] == nil {
				cams[cl] = map[string]int{}
			}
			cams[cl][tzCamera(a)]++
		}
		if class != "" && slices.Contains(classes, class) {
			var tz *string
			if a.ExifInfo != nil {
				tz = a.ExifInfo.TimeZone
			}
			listed = append(listed, tzAuditAsset{
				ID: a.Id.String(), OriginalFileName: a.OriginalFileName, TimeZone: tz,
				LocalDateTime: a.LocalDateTime.UTC().Format("2006-01-02 15:04:05"), Camera: tzCamera(a), Classes: classes,
			})
		}
	}

	switch {
	case idsOnly:
		for _, l := range listed {
			fmt.Fprintln(w, l.ID)
		}
		return nil
	case asJSON:
		out := struct {
			Total     int            `json:"total"`
			Classes   map[string]int `json:"classes"`
			TimeZones map[string]int `json:"timeZones"`
			Assets    []tzAuditAsset `json:"assets,omitempty"`
		}{len(assets), counts, zones, listed}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	fmt.Fprintf(w, "Time zone audit: %d asset(s)\n", len(assets))
	for _, cl := range workflows.TZClasses {
		if cl == workflows.TZClassDSTMismatch && ref == nil {
			fmt.Fprintf(w, "  %-18s %8s  (needs --reference-zone)\n", cl, "-")
			continue
		}
		fmt.Fprintf(w, "  %-18s %8d\n", cl, counts[cl])
		for i, k := range byCountDesc(cams[cl]) {
			if i >= top {
				break
			}
			fmt.Fprintf(w, "      %8d  %s\n", cams[cl][k], k)
		}
	}
	fmt.Fprintf(w, "\nTime zones: %d distinct\n", len(zones))
	for _, k := range byCountDesc(zones) {
		fmt.Fprintf(w, "  %8d  %s\n", zones[k], k)
	}
	if class != "" {
		fmt.Fprintf(w, "\n%s: %d asset(s)\n", class, len(listed))
		for _, l := range listed {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", l.ID, l.OriginalFileName, deref(l.TimeZone), l.LocalDateTime, l.Camera)
		}
	}
	return nil
}

func clientWorkflowTZSet(ctx context.Context, cmd *cli.Command) error {
	loc, err := workflows.LoadIANAZone(cmd.String("time-zone"))
	if err != nil {
		return fmt.Errorf("--time-zone: %w", err)
	}
	dryRun, yes := cmd.Bool("dry-run"), cmd.Bool("yes")
	hasIDs := cmd.Args().Len() > 0 || cmd.String("ids-file") != ""
	if hasIDs {
		for _, f := range searchFilterFlags() {
			if cmd.IsSet(f.Names()[0]) {
				return fmt.Errorf("pass asset IDs or search filters (--%s), not both", f.Names()[0])
			}
		}
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	var assets []immichapi.AssetResponseDto
	var fetchErr error
	if hasIDs {
		ids, err := collectIDs(cmd)
		if err != nil {
			return err
		}
		// Fresh GET /assets/{id} per asset: an ID list may be stale, and
		// SplitTZSet must see the current zone to never overwrite one.
		assets, fetchErr = workflows.FetchAssets(ctx, c, ids)
	} else {
		all, err := tzSearch(ctx, cmd, c)
		if err != nil {
			return err
		}
		for _, a := range all {
			if workflows.ClassifyTZ(a, nil)[0] == workflows.TZClassNone {
				assets = append(assets, a)
			}
		}
	}

	todo, skips := workflows.SplitTZSet(assets, loc)
	for _, s := range skips {
		fmt.Printf("skip %s\n", s)
	}
	fmt.Printf("%d asset(s) to set to %s, %d skipped\n", len(todo), loc, len(skips))
	if len(todo) == 0 {
		return fetchErr
	}
	if !dryRun && !yes {
		fmt.Printf("Set the time zone of %d asset(s) to %s (wall-clock time kept)? [y/N] ", len(todo), loc)
		if !confirm(os.Stdin) {
			return joinErrs(fmt.Errorf("aborted (pass --yes when piping IDs via stdin)"), fetchErr)
		}
	}
	changed, err := workflows.SetAssetsTimeZone(ctx, c, todo, loc, dryRun)
	verb := "changed"
	if dryRun {
		verb = "would change"
	}
	fmt.Printf("%s %d asset(s)\n", verb, changed)
	return joinErrs(err, fetchErr)
}
