package workflows

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // embed the IANA database: LoadLocation must work on hosts without one (Windows)

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// Time zone classes reported by tz-audit. Every asset has exactly one primary
// class (none, utc, offset, iana); the anomaly classes come on top.
const (
	TZClassNone             = "none"              // exifInfo.timeZone is null
	TZClassUTC              = "utc"               // plain "UTC": EXIF had no offset, wall clock treated as UTC
	TZClassOffset           = "offset"            // fixed offset, e.g. "UTC+2"
	TZClassIANA             = "iana"              // IANA name, e.g. "Europe/Berlin"
	TZClassDSTMismatch      = "dst-mismatch"      // fixed offset is the reference zone's *other* offset at that date
	TZClassIANAInconsistent = "iana-inconsistent" // IANA offset ≠ localDateTime − fileCreatedAt
)

// TZClasses lists every class in report order.
var TZClasses = []string{TZClassNone, TZClassUTC, TZClassOffset, TZClassIANA, TZClassDSTMismatch, TZClassIANAInconsistent}

// LoadIANAZone validates a user-supplied IANA zone name. time.LoadLocation
// alone is too lenient: it maps "" to UTC and "Local" to the host zone.
func LoadIANAZone(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, fmt.Errorf("invalid time zone %q: expected an IANA name like Europe/Berlin", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("invalid time zone %q: expected an IANA name like Europe/Berlin (fixed offsets like UTC+1 are not accepted)", name)
	}
	return loc, nil
}

// offsetRe matches Immich's fixed-offset zone names: "UTC+2", "UTC-5", "UTC+5:30", "UTC+0".
var offsetRe = regexp.MustCompile(`^UTC([+-])(\d{1,2})(?::(\d{2}))?$`)

// parseFixedOffset returns the offset in seconds of an Immich "UTC±H[:MM]" name.
func parseFixedOffset(s string) (int, bool) {
	m := offsetRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	h, _ := strconv.Atoi(m[2])
	mins, _ := strconv.Atoi(m[3]) // "" → 0
	off := h*3600 + mins*60
	if m[1] == "-" {
		off = -off
	}
	return off, true
}

// assetTZ returns the asset's stored time zone, or "" when it has none.
func assetTZ(a immichapi.AssetResponseDto) string {
	if a.ExifInfo == nil || a.ExifInfo.TimeZone == nil {
		return ""
	}
	return *a.ExifInfo.TimeZone
}

// wallClockIn reinterprets the asset's localDateTime (the wall clock Immich
// shows, stored with a UTC label) as a wall-clock time in loc.
func wallClockIn(a immichapi.AssetResponseDto, loc *time.Location) time.Time {
	l := a.LocalDateTime.UTC()
	return time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute(), l.Second(), l.Nanosecond(), loc)
}

// ClassifyTZ returns the classes an asset belongs to: its primary class plus
// any anomaly classes. ref (optional) enables dst-mismatch: a fixed offset
// that is one of ref's two offsets that year, but not the one in effect on
// that date (e.g. UTC+2 in January for Europe/Berlin). Pure.
func ClassifyTZ(a immichapi.AssetResponseDto, ref *time.Location) []string {
	tz := assetTZ(a)
	switch {
	case a.ExifInfo == nil || a.ExifInfo.TimeZone == nil:
		return []string{TZClassNone}
	case tz == "UTC":
		return []string{TZClassUTC}
	}
	if off, ok := parseFixedOffset(tz); ok {
		classes := []string{TZClassOffset}
		if ref != nil {
			w := wallClockIn(a, ref)
			_, want := w.Zone()
			_, jan := time.Date(w.Year(), time.January, 1, 12, 0, 0, 0, ref).Zone()
			_, jul := time.Date(w.Year(), time.July, 1, 12, 0, 0, 0, ref).Zone()
			if off != want && jan != jul && (off == jan || off == jul) {
				classes = append(classes, TZClassDSTMismatch)
			}
		}
		return classes
	}
	classes := []string{TZClassIANA}
	if loc, err := time.LoadLocation(tz); err == nil && strings.Contains(tz, "/") {
		_, off := a.FileCreatedAt.In(loc).Zone()
		if diff := a.LocalDateTime.Sub(a.FileCreatedAt); diff != time.Duration(off)*time.Second {
			classes = append(classes, TZClassIANAInconsistent)
		}
	}
	return classes
}

// TZSetDate returns the dateTimeOriginal to send so the asset keeps its
// wall-clock time but gets loc's offset in effect at that local date/time
// (DST-aware per asset), e.g. "2020-07-15T12:00:00+02:00". It refuses
// assets that already have a zone or have no dateTimeOriginal to anchor to.
func TZSetDate(a immichapi.AssetResponseDto, loc *time.Location) (string, error) {
	if tz := assetTZ(a); tz != "" {
		return "", fmt.Errorf("already has time zone %s", tz)
	}
	if a.ExifInfo == nil || a.ExifInfo.DateTimeOriginal == nil {
		return "", fmt.Errorf("no dateTimeOriginal to anchor the wall-clock time to")
	}
	return wallClockIn(a, loc).Format(time.RFC3339Nano), nil
}

// SearchAssetsWithExif pages through POST /search/metadata with withExif set
// (trash excluded), using body's filter. Each page is reported to progress
// (may be nil) so library-wide scans are not silent.
func SearchAssetsWithExif(ctx context.Context, c *client.Client, body immichapi.MetadataSearchDto, progress func(n int)) ([]immichapi.AssetResponseDto, error) {
	withExif, size := true, 1000
	body.WithExif, body.Size = &withExif, &size
	var out []immichapi.AssetResponseDto
	var pager SearchPager
	for {
		pager.Apply(&body)
		resp, err := SearchExcludingTrash(ctx, c, body)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err == nil && resp.JSON200 == nil {
			err = fmt.Errorf("response had no body")
		}
		if err != nil {
			return out, fmt.Errorf("POST /search/metadata: %w", err)
		}
		out = append(out, resp.JSON200.Assets.Items...)
		if progress != nil {
			progress(len(out))
		}
		if !pager.Next(resp.JSON200.Assets) {
			return out, nil
		}
	}
}

// SplitTZSet separates the assets tz-set may change from the ones it must
// skip (already zoned, or no dateTimeOriginal), with a reason per skip. Pure.
func SplitTZSet(assets []immichapi.AssetResponseDto, loc *time.Location) (todo []immichapi.AssetResponseDto, skips []string) {
	for _, a := range assets {
		if _, err := TZSetDate(a, loc); err != nil {
			skips = append(skips, fmt.Sprintf("%s (%s): %v", a.OriginalFileName, a.Id, err))
			continue
		}
		todo = append(todo, a)
	}
	return todo, skips
}

// SetAssetsTimeZone gives every asset in todo (see SplitTZSet) loc's offset
// via PUT /assets/{id} (dateTimeOriginal with explicit offset), keeping the
// wall-clock time. Continue-on-error; dry-run only prints the plan.
//
// Immich v3.4 stores the result as a fixed offset (e.g. "UTC+2"): the
// sidecar-write → metadata re-extraction roundtrip replaces any IANA name
// with the offset written to the XMP sidecar (verified on a staging server).
func SetAssetsTimeZone(ctx context.Context, c *client.Client, todo []immichapi.AssetResponseDto, loc *time.Location, dryRun bool) (changed int, err error) {
	progress := NewProgress(len(todo))
	err = RunBatch(todo,
		func(a immichapi.AssetResponseDto) string { return a.OriginalFileName },
		func(a immichapi.AssetResponseDto) error {
			if !dryRun {
				progress.Step(a.OriginalFileName)
			}
			d, err := TZSetDate(a, loc)
			if err != nil {
				return err
			}
			steps := []Step{{
				Name: fmt.Sprintf("Set dateTimeOriginal %s (%s, wall clock %s unchanged)", d, loc, a.LocalDateTime.UTC().Format("2006-01-02 15:04:05")),
				Run: func(ctx context.Context) error {
					resp, err := c.API.UpdateAssetWithResponse(ctx, a.Id, immichapi.UpdateAssetDto{DateTimeOriginal: &d})
					if err == nil {
						err = client.Check(resp, http.StatusOK)
					}
					return err
				},
			}}
			if err := RunSteps(ctx, RunOptions{DryRun: dryRun}, a.OriginalFileName, steps); err != nil {
				return err
			}
			changed++
			return nil
		},
	)
	return changed, err
}
