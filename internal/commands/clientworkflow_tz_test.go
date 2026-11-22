package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func TestPrintTZAuditListsTimeZones(t *testing.T) {
	asset := func(tz *string) immichapi.AssetResponseDto {
		return immichapi.AssetResponseDto{ExifInfo: &immichapi.ExifResponseDto{TimeZone: tz}}
	}
	berlin, utc2 := "Europe/Berlin", "UTC+2"
	assets := []immichapi.AssetResponseDto{asset(&utc2), asset(&berlin), asset(&berlin), asset(nil)}

	var buf bytes.Buffer
	if err := printTZAudit(&buf, assets, nil, "", false, false, 5); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	_, zones, ok := strings.Cut(out, "Time zones: 3 distinct\n")
	if !ok {
		t.Fatalf("no time zone section:\n%s", out)
	}
	want := "         2  Europe/Berlin\n         1  (none)\n         1  UTC+2\n"
	if zones != want {
		t.Errorf("time zones =\n%q\nwant\n%q", zones, want)
	}
}
