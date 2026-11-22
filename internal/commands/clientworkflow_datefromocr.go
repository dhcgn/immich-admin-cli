package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

func dateFromOCRCommand() *cli.Command {
	return &cli.Command{
		Name:      "date-from-ocr",
		Usage:     "Set an asset's capture year from a year Immich's OCR found in the image (e.g. scanned prints)",
		ArgsUsage: "[ASSET_ID|ASSET_URL ...]",
		Description: "Reads the OCR text boxes (GET /assets/{id}/ocr) of every asset in --album-id/--album-name, " +
			"of the given asset IDs/links, or with --all of the whole library (pre-filtered server-side by one " +
			"OCR search per year, so only likely hits are fetched), and finds 4-digit years (1800–current year) in boxes with " +
			"textScore ≥ --min-score. Each hit is printed with its web link. An asset whose single OCR year " +
			"differs from its capture year is a 'change' candidate; the fix sets the capture date to " +
			"YYYY-06-01 00:00 (mid-year) and tags the asset " + workflows.DateFromOCRTag + ".\n" +
			"Without flags it only reports. --yes applies the unambiguous single-asset changes only " +
			"(never whole albums). --interactive (terminal required) walks the hits grouped by album with " +
			"[s]kip, [t]his asset → YYYY, [a]lbum → YYYY (lists every asset with old/new date and asks again; " +
			"album-propagated assets are tagged " + workflows.DateFromOCRAlbumTag + "), [n]ext album (skip the " +
			"rest of this album's hits; an asset also in another album is still shown there), [y] enter another year, " +
			"[o]pen in browser, [q]uit.\n" +
			"Setting the date uses the deprecated PUT /assets/{id} (updateAsset), the only Immich API that can " +
			"set a capture date (documented exception).",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "album-id", Usage: "scan every asset in this album `UUID`"},
			&cli.StringFlag{Name: "album-name", Usage: "scan every asset in the album with this exact `NAME`"},
			idsFileFlag(),
			&cli.BoolFlag{
				Name:  "all",
				Usage: "scan the whole library: one server-side OCR search per year (1800–now, trash excluded), then check each hit; report or --interactive only (not with --yes), interactive skips already-matching assets",
			},
			&cli.FloatFlag{
				Name:  "min-score",
				Usage: "minimum OCR textScore (0–1) for a text box to count",
				Value: workflows.DefaultOCRMinScore,
			},
			&cli.BoolFlag{
				Name:    "interactive",
				Aliases: []string{"i"},
				Usage:   "review each hit grouped by album: [s]kip [t]his [a]lbum [n]ext album [y]ear [o]pen [q]uit (needs a terminal)",
			},
			&cli.BoolFlag{Name: "dry-run", Usage: "never change anything (also inside --interactive)"},
			&cli.BoolFlag{Name: "yes", Usage: "apply the unambiguous single-asset changes without asking (never whole albums)"},
			&cli.BoolFlag{Name: "json", Usage: "print the report as JSON (report only; not with --yes/--interactive)"},
		},
		Action: clientWorkflowDateFromOCR,
	}
}

func clientWorkflowDateFromOCR(ctx context.Context, cmd *cli.Command) error {
	albumIDStr, albumName := cmd.String("album-id"), cmd.String("album-name")
	interactive, yes, dryRun, asJSON := cmd.Bool("interactive"), cmd.Bool("yes"), cmd.Bool("dry-run"), cmd.Bool("json")
	hasIDs := cmd.Args().Len() > 0 || cmd.String("ids-file") != ""
	all := cmd.Bool("all")

	if albumIDStr != "" && albumName != "" {
		return fmt.Errorf("pass only one of --album-id or --album-name")
	}
	sources := 0
	for _, set := range []bool{albumIDStr != "" || albumName != "", hasIDs, all} {
		if set {
			sources++
		}
	}
	if sources != 1 {
		return fmt.Errorf("pass exactly one source: --album-id/--album-name, asset IDs/links (arguments or --ids-file), or --all")
	}
	if all && yes {
		// Library-wide hits include many non-dates ("seit 1938", "2000 qm",
		// "Geeicht bis 2021"); measured on a real library: ~470 'change' hits,
		// a large share of them false. Review them instead of bulk-applying.
		return fmt.Errorf("--all cannot be combined with --yes (library-wide OCR hits include many non-date numbers); use --all --interactive, or review --all --json and pass the chosen IDs via --ids-file with --yes")
	}
	if interactive && yes {
		return fmt.Errorf("--interactive and --yes are mutually exclusive")
	}
	if asJSON && (interactive || yes) {
		return fmt.Errorf("--json is report-only and cannot be combined with --interactive or --yes")
	}
	if interactive && !workflows.IsTerminal(os.Stdin) {
		return fmt.Errorf("--interactive needs a terminal on stdin; use the report (no flags) or --yes instead")
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Collect the assets to scan.
	var assets []immichapi.AssetResponseDto
	var album *immichapi.AlbumResponseDto
	var fetchErr error
	switch {
	case all:
		if assets, err = workflows.FindAssetsWithOCRYears(ctx, c, time.Now().Year()); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "OCR search found %d candidate asset(s); checking their OCR text...\n", len(assets))
	case hasIDs:
		ids, err := collectIDs(cmd)
		if err != nil {
			return err
		}
		assets, fetchErr = workflows.FetchAssets(ctx, c, ids)
	default:
		var albumID *openapi_types.UUID
		if albumIDStr != "" {
			id, err := uuid.Parse(albumIDStr)
			if err != nil {
				return fmt.Errorf("invalid --album-id %q: %w", albumIDStr, err)
			}
			albumID = &id
		}
		a, err := resolveDownloadAlbum(ctx, c, albumID, albumName, yes)
		if err != nil {
			return err
		}
		album = &a
		if assets, err = workflows.FetchAlbumAssets(ctx, c, a.Id); err != nil {
			return err
		}
	}

	cands, scanErr := workflows.ScanOCRYears(ctx, c, assets, cmd.Float("min-score"))
	slices.SortFunc(cands, func(a, b workflows.OCRYearCandidate) int {
		return strings.Compare(a.Asset.OriginalFileName, b.Asset.OriginalFileName)
	})

	if asJSON {
		return joinErrs(printOCRReportJSON(os.Stdout, cands, c.Server), fetchErr, scanErr)
	}

	if interactive {
		if all {
			// Library-wide, already-matching assets would mean hundreds of
			// pointless prompts; only review what may need a change.
			cands = withoutMatching(cands)
		}
		fmt.Println("Note: setting dates uses the deprecated PUT /assets/{id} endpoint — Immich has no replacement for setting a capture date.")
		groups, err := groupOCRCandidates(cands, album, func(id openapi_types.UUID) ([]immichapi.AlbumResponseDto, error) {
			return workflows.AlbumsForAsset(ctx, c, id)
		})
		if err != nil {
			return err
		}
		r := &ocrReview{
			in: bufio.NewScanner(os.Stdin), out: os.Stdout, server: c.Server,
			albumAssets: func(id openapi_types.UUID) ([]immichapi.AssetResponseDto, error) {
				return workflows.FetchAlbumAssets(ctx, c, id)
			},
			setYear: func(as []immichapi.AssetResponseDto, year int, tag string) error {
				return workflows.SetAssetsYear(ctx, c, as, year, tag, dryRun)
			},
			open: openBrowser,
		}
		return joinErrs(r.run(groups), fetchErr, scanErr)
	}

	changes := printOCRReport(os.Stdout, cands, len(assets), c.Server)
	if len(changes) == 0 || dryRun || !yes {
		if len(changes) > 0 && !dryRun {
			fmt.Println("Report only. Rerun with --yes to apply the single-asset changes above, or --interactive to review (incl. whole albums).")
		}
		return joinErrs(fetchErr, scanErr)
	}
	fmt.Println("Note: setting dates uses the deprecated PUT /assets/{id} endpoint — Immich has no replacement for setting a capture date.")
	failures := 0
	for _, cand := range changes {
		y, _ := cand.ProposedYear()
		if err := workflows.SetAssetsYear(ctx, c, []immichapi.AssetResponseDto{cand.Asset}, y, workflows.DateFromOCRTag, false); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", cand.Asset.OriginalFileName, err)
			failures++
		}
	}
	var applyErr error
	if failures > 0 {
		applyErr = fmt.Errorf("%d of %d change(s) failed", failures, len(changes))
	}
	return joinErrs(applyErr, fetchErr, scanErr)
}

// withoutMatching drops candidates whose OCR year already equals the capture
// year (status "matches").
func withoutMatching(cands []workflows.OCRYearCandidate) []workflows.OCRYearCandidate {
	return slices.DeleteFunc(slices.Clone(cands), func(c workflows.OCRYearCandidate) bool {
		return c.Status() == workflows.OCRStatusMatches
	})
}

// joinErrs returns the first non-nil error; later ones were already logged.
func joinErrs(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func assetLink(server string, id openapi_types.UUID) string {
	return fmt.Sprintf("%s/photos/%s", server, id)
}

const ocrTimeFmt = "2006-01-02 15:04:05"

// printOCRReport prints one block per candidate and returns the "change"
// candidates (the ones --yes would apply).
func printOCRReport(w io.Writer, cands []workflows.OCRYearCandidate, scanned int, server string) []workflows.OCRYearCandidate {
	var changes []workflows.OCRYearCandidate
	matches, ambiguous := 0, 0
	for _, cand := range cands {
		a := cand.Asset
		line := fmt.Sprintf("%-9s  %s  OCR %q  now %s", cand.Status(), a.OriginalFileName, strings.Join(cand.Texts, " | "), a.LocalDateTime.Format(ocrTimeFmt))
		switch cand.Status() {
		case workflows.OCRStatusChange:
			y, _ := cand.ProposedYear()
			line += " -> " + workflows.OCRTargetDate(y).Format(ocrTimeFmt)
			changes = append(changes, cand)
		case workflows.OCRStatusMatches:
			matches++
		default:
			line += fmt.Sprintf("  (years %v, skipped)", cand.Years)
			ambiguous++
		}
		fmt.Fprintln(w, line)
		fmt.Fprintf(w, "           %s\n", assetLink(server, a.Id))
	}
	fmt.Fprintf(w, "--- %d asset(s) scanned, %d with an OCR year: %d change, %d already matching, %d ambiguous ---\n",
		scanned, len(cands), len(changes), matches, ambiguous)
	return changes
}

type ocrReportRow struct {
	AssetID       openapi_types.UUID `json:"assetId"`
	FileName      string             `json:"originalFileName"`
	Link          string             `json:"link"`
	LocalDateTime string             `json:"localDateTime"`
	OCRTexts      []string           `json:"ocrTexts"`
	OCRYears      []int              `json:"ocrYears"`
	Status        string             `json:"status"`
	TargetDate    string             `json:"targetDate,omitempty"`
}

func printOCRReportJSON(w io.Writer, cands []workflows.OCRYearCandidate, server string) error {
	rows := make([]ocrReportRow, 0, len(cands))
	for _, cand := range cands {
		row := ocrReportRow{
			AssetID: cand.Asset.Id, FileName: cand.Asset.OriginalFileName, Link: assetLink(server, cand.Asset.Id),
			LocalDateTime: cand.Asset.LocalDateTime.Format(ocrTimeFmt), OCRTexts: cand.Texts, OCRYears: cand.Years,
			Status: cand.Status(),
		}
		if row.Status == workflows.OCRStatusChange {
			row.TargetDate = workflows.OCRTargetDate(cand.Years[0]).Format(ocrTimeFmt)
		}
		rows = append(rows, row)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

// ocrGroup is the interactive review unit: the candidates of one album
// (Album nil = assets in no album, where only [t]his is possible).
type ocrGroup struct {
	Album *immichapi.AlbumResponseDto
	Cands []workflows.OCRYearCandidate
}

// groupOCRCandidates groups candidates by album. With a known source album
// every candidate belongs to it; otherwise albumsOf is asked per candidate
// and an asset in several albums appears in each of them.
func groupOCRCandidates(cands []workflows.OCRYearCandidate, album *immichapi.AlbumResponseDto, albumsOf func(openapi_types.UUID) ([]immichapi.AlbumResponseDto, error)) ([]ocrGroup, error) {
	if len(cands) == 0 {
		return nil, nil
	}
	if album != nil {
		return []ocrGroup{{Album: album, Cands: cands}}, nil
	}
	var groups []ocrGroup
	index := map[openapi_types.UUID]int{}
	var noAlbum []workflows.OCRYearCandidate
	for _, cand := range cands {
		albums, err := albumsOf(cand.Asset.Id)
		if err != nil {
			return nil, err
		}
		if len(albums) == 0 {
			noAlbum = append(noAlbum, cand)
		}
		for _, a := range albums {
			i, ok := index[a.Id]
			if !ok {
				i = len(groups)
				index[a.Id] = i
				groups = append(groups, ocrGroup{Album: &a})
			}
			groups[i].Cands = append(groups[i].Cands, cand)
		}
	}
	slices.SortFunc(groups, func(a, b ocrGroup) int { return strings.Compare(a.Album.AlbumName, b.Album.AlbumName) })
	if len(noAlbum) > 0 {
		groups = append(groups, ocrGroup{Cands: noAlbum})
	}
	return groups, nil
}

// ocrReview drives the interactive review. All side effects are injected so
// the prompt flow — in particular the album re-confirmation — is testable.
type ocrReview struct {
	in          *bufio.Scanner
	out         io.Writer
	server      string
	albumAssets func(openapi_types.UUID) ([]immichapi.AssetResponseDto, error)
	setYear     func(assets []immichapi.AssetResponseDto, year int, tag string) error
	open        func(url string) error

	done     map[openapi_types.UUID]bool // assets already changed in this run
	failures int
}

func (r *ocrReview) run(groups []ocrGroup) error {
	r.done = map[openapi_types.UUID]bool{}
	if len(groups) == 0 {
		fmt.Fprintln(r.out, "No asset with an OCR year found.")
		return nil
	}
	changed := 0
groupsLoop:
	for _, g := range groups {
		if g.Album != nil {
			fmt.Fprintf(r.out, "\nAlbum %q (%d asset(s), %d with an OCR year)  %s/albums/%s\n", g.Album.AlbumName, g.Album.AssetCount, len(g.Cands), r.server, g.Album.Id)
		} else {
			fmt.Fprintf(r.out, "\nAssets in no album (%d with an OCR year)\n", len(g.Cands))
		}
		for i, cand := range g.Cands {
			if r.done[cand.Asset.Id] {
				continue
			}
			n, quit, albumDone := r.reviewOne(g, i, cand)
			changed += n
			if quit {
				break groupsLoop
			}
			if albumDone {
				break
			}
		}
	}
	fmt.Fprintf(r.out, "--- %d asset(s) changed ---\n", changed)
	if r.failures > 0 {
		return fmt.Errorf("%d change(s) failed", r.failures)
	}
	return nil
}

// reviewOne prompts for one candidate until it is decided. It returns the
// number of assets changed, whether to quit, and whether the rest of the
// album is done (after an [a]lbum change).
func (r *ocrReview) reviewOne(g ocrGroup, i int, cand workflows.OCRYearCandidate) (int, bool, bool) {
	a := cand.Asset
	year, haveYear := cand.ProposedYear()
	for {
		fmt.Fprintf(r.out, "  [%d/%d] %s   OCR %q   now %s", i+1, len(g.Cands), a.OriginalFileName, strings.Join(cand.Texts, " | "), a.LocalDateTime.Format(ocrTimeFmt))
		if haveYear && year != a.LocalDateTime.Year() {
			fmt.Fprintf(r.out, " -> %s", workflows.OCRTargetDate(year).Format(ocrTimeFmt))
		} else if haveYear {
			fmt.Fprintf(r.out, "  (year %d matches)", year)
		} else {
			fmt.Fprintf(r.out, "  (several years %v)", cand.Years)
		}
		fmt.Fprintf(r.out, "\n        %s\n        ", assetLink(r.server, a.Id))

		opts := []string{"[s]kip"}
		if haveYear && year != a.LocalDateTime.Year() {
			opts = append(opts, fmt.Sprintf("[t]his -> %d", year))
		}
		if haveYear && g.Album != nil {
			opts = append(opts, fmt.Sprintf("[a]lbum -> %d", year))
		}
		opts = append(opts, "[n]ext album (skip rest)", "[y]ear...", "[o]pen", "[q]uit")
		fmt.Fprintf(r.out, "%s: ", strings.Join(opts, "  "))

		answer, ok := r.read()
		if !ok {
			return 0, true, false
		}
		switch answer {
		case "", "s", "skip":
			return 0, false, false
		case "n", "next":
			fmt.Fprintf(r.out, "        Skipping the remaining %d hit(s) of this album.\n", len(g.Cands)-i)
			return 0, false, true
		case "q", "quit":
			return 0, true, false
		case "o", "open":
			if err := r.open(assetLink(r.server, a.Id)); err != nil {
				fmt.Fprintf(r.out, "Could not open browser: %v\n", err)
			}
		case "y", "year":
			fmt.Fprintf(r.out, "        Year (1800-%d): ", time.Now().Year())
			s, ok := r.read()
			if !ok {
				return 0, true, false
			}
			y, err := strconv.Atoi(s)
			if err != nil || y < 1800 || y > time.Now().Year() {
				fmt.Fprintf(r.out, "        Invalid year %q.\n", s)
				continue
			}
			year, haveYear = y, true
		case "t", "this":
			if !haveYear || year == a.LocalDateTime.Year() {
				fmt.Fprintln(r.out, "        Nothing to change for this asset.")
				continue
			}
			return r.apply([]immichapi.AssetResponseDto{a}, year, workflows.DateFromOCRTag), false, false
		case "a", "album":
			if !haveYear || g.Album == nil {
				fmt.Fprintln(r.out, "        No album change possible here.")
				continue
			}
			if n, applied := r.confirmAlbum(g.Album, a, year); applied {
				return n, false, true
			}
		default:
			fmt.Fprintf(r.out, "        Unknown choice %q.\n", answer)
		}
	}
}

// confirmAlbum lists every asset of the album with its current and new date,
// asks again (default No) and applies on "y". The triggering asset is tagged
// as OCR evidence, the rest as album-propagated. applied=false sends the
// caller back to the options for the current asset.
func (r *ocrReview) confirmAlbum(album *immichapi.AlbumResponseDto, trigger immichapi.AssetResponseDto, year int) (int, bool) {
	assets, err := r.albumAssets(album.Id)
	if err != nil {
		fmt.Fprintf(r.out, "        Could not list album assets: %v\n", err)
		return 0, false
	}
	slices.SortFunc(assets, func(a, b immichapi.AssetResponseDto) int { return a.LocalDateTime.Compare(b.LocalDateTime) })
	target := workflows.OCRTargetDate(year).Format(ocrTimeFmt)

	var self, others []immichapi.AssetResponseDto
	fmt.Fprintf(r.out, "\n  Album %q: setting the year to %d would change:\n", album.AlbumName, year)
	for _, a := range assets {
		if a.LocalDateTime.Year() == year {
			fmt.Fprintf(r.out, "    keep    %-40s  %s  (year matches)\n", a.OriginalFileName, a.LocalDateTime.Format(ocrTimeFmt))
			continue
		}
		fmt.Fprintf(r.out, "    change  %-40s  %s -> %s\n", a.OriginalFileName, a.LocalDateTime.Format(ocrTimeFmt), target)
		if a.Id == trigger.Id {
			self = append(self, a)
		} else {
			others = append(others, a)
		}
	}
	total := len(self) + len(others)
	if total == 0 {
		fmt.Fprintln(r.out, "  Nothing to change: every asset is already in that year.")
		return 0, false
	}
	fmt.Fprintf(r.out, "  Set %d of %d asset(s) to %s and tag them? [y/N]: ", total, len(assets), target)
	answer, ok := r.read()
	if !ok || (answer != "y" && answer != "yes") {
		fmt.Fprintln(r.out, "  Not changed.")
		return 0, false
	}
	n := r.apply(self, year, workflows.DateFromOCRTag) + r.apply(others, year, workflows.DateFromOCRAlbumTag)
	for _, a := range assets {
		r.done[a.Id] = true
	}
	return n, true
}

func (r *ocrReview) apply(assets []immichapi.AssetResponseDto, year int, tag string) int {
	if len(assets) == 0 {
		return 0
	}
	if err := r.setYear(assets, year, tag); err != nil {
		fmt.Fprintf(r.out, "Error: %v\n", err)
		r.failures++
		return 0
	}
	for _, a := range assets {
		r.done[a.Id] = true
	}
	return len(assets)
}

func (r *ocrReview) read() (string, bool) {
	if !r.in.Scan() {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(r.in.Text())), true
}

// openBrowser opens url in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
