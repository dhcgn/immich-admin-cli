package workflows

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// PatternKind identifies which date-name convention an album matched.
type PatternKind string

const (
	// PatternDay is an album named "yyyy-MM-dd <title>", e.g. "2025-07-04 Garten".
	PatternDay PatternKind = "day"
	// PatternYear is an album named "yyyy <title>", e.g. "2010 USA".
	PatternYear PatternKind = "year"
)

// AlbumDatePattern is the date range implied by a date-pattern album name.
// To is an exclusive upper bound.
type AlbumDatePattern struct {
	Kind PatternKind
	From time.Time
	To   time.Time
}

// dayPattern matches "yyyy-MM-dd" optionally followed by whitespace and a title.
var dayPattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})(?:\s+.*)?$`)

// yearPattern matches "yyyy" optionally followed by whitespace and a title. A
// day-pattern name (e.g. "2025-07-04 Garten") never matches this: the
// character right after the 4 digits is '-', not whitespace or end-of-string.
var yearPattern = regexp.MustCompile(`^(\d{4})(?:\s+.*)?$`)

// parseAlbumDatePattern classifies an album name as a day-precision or
// year-precision date album and reports the calendar range it implies. It is
// pure (no network) so the classification logic can be unit-tested directly.
// A name that merely looks like a year but isn't a real calendar date (e.g.
// "2025-13-40 Foo") is reported as no match rather than an error — it simply
// isn't a date-pattern album.
func parseAlbumDatePattern(name string) (AlbumDatePattern, bool) {
	if m := dayPattern.FindStringSubmatch(name); m != nil {
		from, err := time.ParseInLocation("2006-01-02", m[1], time.UTC)
		if err != nil {
			return AlbumDatePattern{}, false
		}
		return AlbumDatePattern{Kind: PatternDay, From: from, To: from.AddDate(0, 0, 1)}, true
	}
	if m := yearPattern.FindStringSubmatch(name); m != nil {
		from, err := time.ParseInLocation("2006", m[1], time.UTC)
		if err != nil {
			return AlbumDatePattern{}, false
		}
		return AlbumDatePattern{Kind: PatternYear, From: from, To: from.AddDate(1, 0, 0)}, true
	}
	return AlbumDatePattern{}, false
}

// FixAlbumDatesOptions controls the fix-album-dates workflow.
type FixAlbumDatesOptions struct {
	// DryRun prints the planned date fixes without calling the API.
	DryRun bool
	// OffsetDays widens the accepted range by this many days on each side
	// before flagging an asset as a mismatch (e.g. 1 means an asset up to a
	// day before or after the nominal range is still considered in range).
	// This absorbs boundary discrepancies caused by camera/EXIF timezone or
	// DST mismatches near midnight — see parseAlbumDatePattern and
	// findDateMismatches for why LocalDateTime itself needs no timezone
	// conversion. It does not affect the fix target, which is always the
	// pattern's exact nominal date (see ComputeFixedDateTime).
	OffsetDays int
}

// DefaultOffsetDays is the recommended FixAlbumDatesOptions.OffsetDays value
// ("two days plus/minus"), used as the command-line flag default.
const DefaultOffsetDays = 2

// AlbumDateCheck is one date-pattern album together with the assets whose
// LocalDateTime falls outside the range implied by its name. Mismatches is
// empty for an album where every asset matches.
type AlbumDateCheck struct {
	Album      immichapi.AlbumResponseDto
	Pattern    AlbumDatePattern
	Mismatches []immichapi.AssetResponseDto
}

// CheckAlbumDates fetches all albums (GET /albums), keeps the ones whose name
// matches the day or year date pattern, and for each fetches its assets (POST
// /search/metadata, scoped by album) to find any whose LocalDateTime falls
// outside the range implied by the album name. It performs no writes — this
// is the read-only "getting the information" path used both by the command's
// review step and by the integration test. Every date-pattern album is
// returned, ordered worst-first (the album with the single largest
// out-of-range deviation first; see maxDeviation), including ones with zero
// mismatches at the end.
func CheckAlbumDates(ctx context.Context, c *client.Client, opts FixAlbumDatesOptions) ([]AlbumDateCheck, error) {
	resp, err := c.API.GetAllAlbumsWithResponse(ctx, &immichapi.GetAllAlbumsParams{})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching albums: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("fetching albums: response had no body")
	}

	var albums []immichapi.AlbumResponseDto
	for _, a := range *resp.JSON200 {
		if _, ok := parseAlbumDatePattern(a.AlbumName); ok {
			albums = append(albums, a)
		}
	}
	sort.Slice(albums, func(i, j int) bool { return albums[i].AlbumName < albums[j].AlbumName })

	checks := make([]AlbumDateCheck, 0, len(albums))
	offset := time.Duration(opts.OffsetDays) * 24 * time.Hour
	for _, a := range albums {
		pattern, _ := parseAlbumDatePattern(a.AlbumName)

		assets, err := fetchAlbumAssets(ctx, c, a.Id)
		if err != nil {
			return nil, fmt.Errorf("fetching assets for album %q: %w", a.AlbumName, err)
		}

		checks = append(checks, AlbumDateCheck{
			Album:      a,
			Pattern:    pattern,
			Mismatches: findDateMismatches(assets, pattern, offset),
		})
	}

	sort.SliceStable(checks, func(i, j int) bool { return maxDeviation(checks[i]) > maxDeviation(checks[j]) })
	return checks, nil
}

// maxDeviation returns how far outside its pattern's range the single
// worst-offending mismatch in check falls (zero if there are no
// mismatches). Used to order the report so the most-likely-wrong albums are
// shown first.
func maxDeviation(check AlbumDateCheck) time.Duration {
	var worst time.Duration
	for _, a := range check.Mismatches {
		if d := deviationFromRange(a.LocalDateTime, check.Pattern); d > worst {
			worst = d
		}
	}
	return worst
}

// deviationFromRange returns how far t falls outside [p.From, p.To), or zero
// if t is inside it.
func deviationFromRange(t time.Time, p AlbumDatePattern) time.Duration {
	if t.Before(p.From) {
		return p.From.Sub(t)
	}
	if !t.Before(p.To) {
		return t.Sub(p.To)
	}
	return 0
}

// fetchAlbumAssets returns every asset in albumID via the album-scoped
// metadata search (POST /search/metadata, MetadataSearchDto.AlbumIds),
// following the NextPage cursor until exhausted. AlbumResponseDto itself
// carries no assets list in this API version.
func fetchAlbumAssets(ctx context.Context, c *client.Client, albumID openapi_types.UUID) ([]immichapi.AssetResponseDto, error) {
	var assets []immichapi.AssetResponseDto
	page := 1
	size := 250
	for {
		body := immichapi.MetadataSearchDto{
			AlbumIds: &[]openapi_types.UUID{albumID},
			Page:     &page,
			Size:     &size,
		}

		resp, err := c.API.SearchAssetsWithResponse(ctx, &immichapi.SearchAssetsParams{}, body)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			return nil, fmt.Errorf("searching album assets: %w", err)
		}
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("searching album assets: response had no body")
		}

		assets = append(assets, resp.JSON200.Assets.Items...)

		next := resp.JSON200.Assets.NextPage
		if next == nil || *next == "" {
			return assets, nil
		}
		nextPage, err := strconv.Atoi(*next)
		if err != nil {
			// Not a plain integer token: stop rather than loop forever.
			return assets, nil
		}
		page = nextPage
	}
}

// findDateMismatches returns the assets whose LocalDateTime falls outside
// [p.From-offset, p.To+offset) — offset is the allowed tolerance in each
// direction (see FixAlbumDatesOptions.OffsetDays). LocalDateTime is already
// the photographer's local time regardless of timezone (Immich derives it
// from EXIF), and p.From/p.To are parsed from the album name the same
// timezone-naive way, so this comparison needs no timezone conversion of its
// own; offset exists only to forgive boundary slack (e.g. a camera whose
// clock/timezone or DST was set wrong around midnight), not to correct
// timezones. It is pure (no network) so the detection logic can be
// unit-tested in isolation.
func findDateMismatches(assets []immichapi.AssetResponseDto, p AlbumDatePattern, offset time.Duration) []immichapi.AssetResponseDto {
	from := p.From.Add(-offset)
	to := p.To.Add(offset)

	var mismatches []immichapi.AssetResponseDto
	for _, a := range assets {
		if a.LocalDateTime.Before(from) || !a.LocalDateTime.Before(to) {
			mismatches = append(mismatches, a)
		}
	}
	return mismatches
}

// ComputeFixedDateTime returns the corrected local timestamp for asset given
// pattern: the album's date combined with the asset's own existing
// hour/minute/second (preserving relative ordering within the album). It
// only applies to day-precision albums — a year-precision album has no
// single unambiguous date to reset an outlier to, so ok is false and no fix
// is offered (report-only).
func ComputeFixedDateTime(asset immichapi.AssetResponseDto, p AlbumDatePattern) (time.Time, bool) {
	if p.Kind != PatternDay {
		return time.Time{}, false
	}
	old := asset.LocalDateTime
	fixed := time.Date(p.From.Year(), p.From.Month(), p.From.Day(), old.Hour(), old.Minute(), old.Second(), old.Nanosecond(), old.Location())
	return fixed, true
}

// FixAlbumDates applies the date fix to every day-precision mismatch found by
// CheckAlbumDates, continuing on error and returning a summary error if any
// fix failed (the same bulk convention used by the other workflows).
// Year-precision albums are report-only per design (see ComputeFixedDateTime)
// and are skipped here with an informational message, never counted as a
// failure. In dry-run mode it prints the planned step for each asset and
// changes nothing.
//
// This is the repo's one deliberate exception to "never use deprecated
// endpoints": PUT /assets/{id} (updateAsset) is the only Immich API endpoint
// that can set an asset's capture date, and it is marked deprecated upstream
// with a self-referential (non-existent) replacementId — no stable
// alternative exists (see .github/copilot-instructions.md).
func FixAlbumDates(ctx context.Context, c *client.Client, checks []AlbumDateCheck, opts FixAlbumDatesOptions) error {
	return RunBatch(checks,
		func(check AlbumDateCheck) string { return check.Album.AlbumName },
		func(check AlbumDateCheck) error {
			if len(check.Mismatches) == 0 {
				return nil
			}
			if check.Pattern.Kind != PatternDay {
				fmt.Printf("%s: %d asset(s) out of range in a year-only album; report only, no automatic fix\n", check.Album.AlbumName, len(check.Mismatches))
				return nil
			}

			return RunBatch(check.Mismatches,
				func(a immichapi.AssetResponseDto) string { return a.OriginalFileName },
				func(a immichapi.AssetResponseDto) error {
					fixed, _ := ComputeFixedDateTime(a, check.Pattern)
					steps := []Step{
						{
							Name: fmt.Sprintf("Set date of %q to %s (was %s)", a.OriginalFileName, fixed.Format("2006-01-02 15:04:05"), a.LocalDateTime.Format("2006-01-02 15:04:05")),
							Run: func(ctx context.Context) error {
								return updateAssetDate(ctx, c, a.Id, fixed)
							},
						},
					}
					return RunSteps(ctx, RunOptions{DryRun: opts.DryRun}, a.OriginalFileName, steps)
				},
			)
		},
	)
}

// updateAssetDate sets a single asset's capture date (PUT /assets/{id},
// UpdateAssetDto.DateTimeOriginal) to t, formatted as a naive local timestamp
// (no timezone suffix) to match EXIF DateTimeOriginal's own timezone-less
// semantics. All other UpdateAssetDto fields are left nil; it is a partial
// update.
func updateAssetDate(ctx context.Context, c *client.Client, id openapi_types.UUID, t time.Time) error {
	s := t.Format("2006-01-02T15:04:05")
	resp, err := c.API.UpdateAssetWithResponse(ctx, id, immichapi.UpdateAssetDto{DateTimeOriginal: &s})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("updating asset date: %w", err)
	}
	return nil
}
