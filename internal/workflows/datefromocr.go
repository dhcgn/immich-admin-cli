package workflows

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

const (
	// DateFromOCRTag marks an asset whose capture year was set from a year
	// OCR found in the asset itself.
	DateFromOCRTag = "immich-admin-cli/date-from-ocr"
	// DateFromOCRAlbumTag marks an asset whose capture year was set because
	// another asset in the same album carried that year (weaker evidence).
	DateFromOCRAlbumTag = "immich-admin-cli/date-from-ocr-album"
	// DefaultOCRMinScore is the default minimum OCR textScore for a box to
	// count as evidence.
	DefaultOCRMinScore = 0.8
)

// OCR candidate statuses.
const (
	OCRStatusChange    = "change"    // exactly one OCR year, differs from the capture year
	OCRStatusMatches   = "matches"   // exactly one OCR year, equals the capture year
	OCRStatusAmbiguous = "ambiguous" // several distinct OCR years
)

// OCRYearCandidate is an asset whose OCR text contains at least one year.
type OCRYearCandidate struct {
	Asset immichapi.AssetResponseDto
	Texts []string // OCR texts that contained a year
	Years []int    // distinct years, ascending
}

// ProposedYear returns the OCR year when exactly one distinct year was found.
func (c OCRYearCandidate) ProposedYear() (int, bool) {
	if len(c.Years) != 1 {
		return 0, false
	}
	return c.Years[0], true
}

// Status classifies the candidate (see the OCRStatus* constants).
func (c OCRYearCandidate) Status() string {
	y, ok := c.ProposedYear()
	switch {
	case !ok:
		return OCRStatusAmbiguous
	case y == c.Asset.LocalDateTime.Year():
		return OCRStatusMatches
	default:
		return OCRStatusChange
	}
}

// OCRTargetDate is the capture date set for a year-only fix: mid-year
// (June 1st, 00:00), so the uncertainty is at most ±6 months.
func OCRTargetDate(year int) time.Time {
	return time.Date(year, time.June, 1, 0, 0, 0, 0, time.UTC)
}

// yearRe matches a standalone 4-digit year 1800–2099, also inside text like
// "Agypten 2004" or "14.07.1986".
var yearRe = regexp.MustCompile(`\b(?:18|19|20)\d{2}\b`)

// ExtractOCRYears returns the distinct years (ascending, ≤ maxYear) found in
// boxes with textScore ≥ minScore, plus the texts they came from. Pure.
func ExtractOCRYears(boxes []immichapi.AssetOcrResponseDto, minScore float64, maxYear int) ([]int, []string) {
	var years []int
	var texts []string
	for _, b := range boxes {
		if b.TextScore < minScore {
			continue
		}
		found := false
		for _, m := range yearRe.FindAllString(b.Text, -1) {
			y, _ := strconv.Atoi(m)
			if y > maxYear {
				continue
			}
			found = true
			if !slices.Contains(years, y) {
				years = append(years, y)
			}
		}
		if found && !slices.Contains(texts, b.Text) {
			texts = append(texts, b.Text)
		}
	}
	slices.Sort(years)
	return years, texts
}

// FetchAssetOCR returns the OCR text boxes of one asset (GET /assets/{id}/ocr).
func FetchAssetOCR(ctx context.Context, c *client.Client, id openapi_types.UUID) ([]immichapi.AssetOcrResponseDto, error) {
	resp, err := c.API.GetAssetOcrWithResponse(ctx, id)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching OCR: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil
	}
	return *resp.JSON200, nil
}

// FetchAssets returns the assets for ids (GET /assets/{id} each), skipping
// and logging per-ID failures; the error summarizes how many failed.
func FetchAssets(ctx context.Context, c *client.Client, ids []openapi_types.UUID) ([]immichapi.AssetResponseDto, error) {
	var out []immichapi.AssetResponseDto
	failures := 0
	for _, id := range ids {
		resp, err := c.API.GetAssetInfoWithResponse(ctx, id, nil)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err == nil && resp.JSON200 == nil {
			err = fmt.Errorf("response had no body")
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: asset %s: %v\n", id, err)
			failures++
			continue
		}
		out = append(out, *resp.JSON200)
	}
	if failures > 0 {
		return out, fmt.Errorf("%d of %d asset(s) could not be fetched", failures, len(ids))
	}
	return out, nil
}

// ScanOCRYears fetches the OCR boxes of every asset and returns the ones
// containing at least one year. Per-asset failures are logged and counted;
// the error summarizes them while the found candidates are still returned.
func ScanOCRYears(ctx context.Context, c *client.Client, assets []immichapi.AssetResponseDto, minScore float64) ([]OCRYearCandidate, error) {
	maxYear := time.Now().Year()
	var out []OCRYearCandidate
	failures := 0
	for i, a := range assets {
		if (i+1)%50 == 0 {
			fmt.Fprintf(os.Stderr, "OCR scanned %d/%d asset(s)...\n", i+1, len(assets))
		}
		boxes, err := FetchAssetOCR(ctx, c, a.Id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", a.OriginalFileName, err)
			failures++
			continue
		}
		if years, texts := ExtractOCRYears(boxes, minScore, maxYear); len(years) > 0 {
			out = append(out, OCRYearCandidate{Asset: a, Texts: texts, Years: years})
		}
	}
	if failures > 0 {
		return out, fmt.Errorf("%d of %d asset(s) failed the OCR fetch", failures, len(assets))
	}
	return out, nil
}

// OCRSearchFromYear is the first year FindAssetsWithOCRYears searches for
// (the same lower bound ExtractOCRYears accepts).
const OCRSearchFromYear = 1800

// FindAssetsWithOCRYears finds library-wide candidates without fetching OCR
// for every asset: one server-side OCR search (filter.ocr, a fuzzy word
// match) per year from OCRSearchFromYear to toYear, trash excluded, results
// de-duplicated. The fuzzy hits still go through ScanOCRYears, which keeps
// only assets whose OCR really contains a year.
func FindAssetsWithOCRYears(ctx context.Context, c *client.Client, toYear int) ([]immichapi.AssetResponseDto, error) {
	seen := map[openapi_types.UUID]bool{}
	var out []immichapi.AssetResponseDto
	size := 1000
	for y := OCRSearchFromYear; y <= toYear; y++ {
		if y%25 == 0 {
			fmt.Fprintf(os.Stderr, "OCR search: year %d of %d..%d, %d candidate(s) so far\n", y, OCRSearchFromYear, toYear, len(out))
		}
		var pager SearchPager
		for {
			body := immichapi.MetadataSearchDto{
				Filter: &immichapi.SearchFilter{Ocr: &immichapi.StringSimilarityFilter{Matches: strconv.Itoa(y)}},
				Size:   &size,
			}
			pager.Apply(&body)
			resp, err := SearchExcludingTrash(ctx, c, body)
			if err == nil {
				err = client.Check(resp, http.StatusOK)
			}
			if err == nil && resp.JSON200 == nil {
				err = fmt.Errorf("response had no body")
			}
			if err != nil {
				return out, fmt.Errorf("OCR search for %d: %w", y, err)
			}
			for _, a := range resp.JSON200.Assets.Items {
				if !seen[a.Id] {
					seen[a.Id] = true
					out = append(out, a)
				}
			}
			if !pager.Next(resp.JSON200.Assets) {
				break
			}
		}
	}
	return out, nil
}

// AlbumsForAsset lists the albums containing an asset (GET /albums?assetId=).
func AlbumsForAsset(ctx context.Context, c *client.Client, id openapi_types.UUID) ([]immichapi.AlbumResponseDto, error) {
	resp, err := c.API.GetAllAlbumsWithResponse(ctx, &immichapi.GetAllAlbumsParams{AssetId: &id})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return nil, fmt.Errorf("listing albums of asset %s: %w", id, err)
	}
	if resp.JSON200 == nil {
		return nil, nil
	}
	return *resp.JSON200, nil
}

// SetAssetsYear sets every asset's capture date to OCRTargetDate(year) via
// updateAsset, then tags the successfully changed assets with tag.
// Continue-on-error; dry-run only prints.
func SetAssetsYear(ctx context.Context, c *client.Client, assets []immichapi.AssetResponseDto, year int, tag string, dryRun bool) error {
	if len(assets) == 0 {
		return nil
	}
	target := OCRTargetDate(year)
	var changed []openapi_types.UUID
	err := RunBatch(assets,
		func(a immichapi.AssetResponseDto) string { return a.OriginalFileName },
		func(a immichapi.AssetResponseDto) error {
			steps := []Step{{
				Name: fmt.Sprintf("Set date to %s (was %s)", target.Format("2006-01-02 15:04:05"), a.LocalDateTime.Format("2006-01-02 15:04:05")),
				Run:  func(ctx context.Context) error { return updateAssetDate(ctx, c, a.Id, target) },
			}}
			if err := RunSteps(ctx, RunOptions{DryRun: dryRun}, a.OriginalFileName, steps); err != nil {
				return err
			}
			changed = append(changed, a.Id)
			return nil
		},
	)
	if dryRun {
		fmt.Printf("[dry-run] would tag %d asset(s) with %q\n", len(changed), tag)
		return err
	}
	if len(changed) == 0 {
		return err
	}
	tagID, terr := ResolveOrCreateTag(ctx, c, tag)
	if terr == nil {
		terr = TagAssets(ctx, c, changed, tagID)
	}
	if terr == nil {
		fmt.Printf("Tagged %d asset(s) with %q\n", len(changed), tag)
	}
	return errors.Join(err, terr)
}
