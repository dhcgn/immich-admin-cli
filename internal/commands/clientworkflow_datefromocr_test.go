package commands

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

func TestParseAssetRef(t *testing.T) {
	const want = "ec905585-077a-401f-9c47-00c84944f563"
	for _, in := range []string{
		want,
		"https://immich.example.com/photos/" + want,
		"https://immich.example.com/people/00554a98-5890-4d4c-b648-bba2ff49708a/photos/" + want + "?previousRoute=%2Fpeople",
	} {
		got, err := parseAssetRef(in)
		if err != nil || got.String() != want {
			t.Errorf("parseAssetRef(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"not-a-uuid", "https://immich.example.com/photos/"} {
		if _, err := parseAssetRef(in); err == nil {
			t.Errorf("parseAssetRef(%q): expected error", in)
		}
	}
}

func testAsset(name string, year int) immichapi.AssetResponseDto {
	return immichapi.AssetResponseDto{
		Id:               openapi_types.UUID(uuid.New()),
		OriginalFileName: name,
		LocalDateTime:    time.Date(year, 11, 15, 18, 46, 15, 0, time.UTC),
	}
}

type setCall struct {
	names []string
	year  int
	tag   string
}

func newTestReview(input string, albumAssets []immichapi.AssetResponseDto) (*ocrReview, *bytes.Buffer, *[]setCall) {
	var out bytes.Buffer
	var calls []setCall
	r := &ocrReview{
		in: bufio.NewScanner(strings.NewReader(input)), out: &out, server: "https://immich.example.com",
		albumAssets: func(openapi_types.UUID) ([]immichapi.AssetResponseDto, error) { return albumAssets, nil },
		setYear: func(as []immichapi.AssetResponseDto, year int, tag string) error {
			c := setCall{year: year, tag: tag}
			for _, a := range as {
				c.names = append(c.names, a.OriginalFileName)
			}
			calls = append(calls, c)
			return nil
		},
		open: func(string) error { return nil },
	}
	return r, &out, &calls
}

func TestOCRReviewThisAsset(t *testing.T) {
	scan := testAsset("440-AngelaDanel-93.jpg", 2009)
	g := ocrGroup{Cands: []workflows.OCRYearCandidate{{Asset: scan, Texts: []string{"1986"}, Years: []int{1986}}}}
	r, out, calls := newTestReview("t\n", nil)
	if err := r.run([]ocrGroup{g}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].year != 1986 || (*calls)[0].tag != workflows.DateFromOCRTag {
		t.Fatalf("calls = %+v, want one 1986 call tagged %s", *calls, workflows.DateFromOCRTag)
	}
	if !strings.Contains(out.String(), "https://immich.example.com/photos/"+scan.Id.String()) {
		t.Errorf("output lacks the asset link:\n%s", out)
	}
	if strings.Contains(out.String(), "[a]lbum") {
		t.Errorf("[a]lbum offered for an asset in no album:\n%s", out)
	}
}

// The album change must list every album asset with its date and only run
// after a second explicit "y"; declining goes back to the options.
func TestOCRReviewAlbumNeedsSecondConfirmation(t *testing.T) {
	scan := testAsset("scan-1986.jpg", 2009)
	other := testAsset("scan-2.jpg", 2009)
	keep := testAsset("already-1986.jpg", 1986)
	album := immichapi.AlbumResponseDto{Id: openapi_types.UUID(uuid.New()), AlbumName: "Fotos Angela", AssetCount: 3}
	g := ocrGroup{Album: &album, Cands: []workflows.OCRYearCandidate{
		{Asset: scan, Texts: []string{"1986"}, Years: []int{1986}},
		{Asset: other, Texts: []string{"1986"}, Years: []int{1986}},
	}}
	albumAssets := []immichapi.AssetResponseDto{scan, other, keep}

	// a → decline (Enter) → back to options → skip; then second asset: skip.
	r, out, calls := newTestReview("a\n\ns\ns\n", albumAssets)
	if err := r.run([]ocrGroup{g}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("declined album change still called setYear: %+v", *calls)
	}
	for _, want := range []string{"change  scan-1986.jpg", "2009-11-15 18:46:15 -> 1986-06-01 00:00:00", "keep    already-1986.jpg", "[y/N]", "Not changed."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// a → y: trigger tagged as OCR evidence, the other as album-propagated,
	// the matching asset untouched, and the album's second hit not asked.
	r, out, calls = newTestReview("a\ny\n", albumAssets)
	if err := r.run([]ocrGroup{g}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 ||
		(*calls)[0].tag != workflows.DateFromOCRTag || strings.Join((*calls)[0].names, ",") != "scan-1986.jpg" ||
		(*calls)[1].tag != workflows.DateFromOCRAlbumTag || strings.Join((*calls)[1].names, ",") != "scan-2.jpg" {
		t.Fatalf("calls = %+v", *calls)
	}
	if strings.Contains(out.String(), "[2/2]") {
		t.Errorf("second hit in an already-changed album was prompted:\n%s", out)
	}
}

func TestOCRReviewOtherYearAndEOF(t *testing.T) {
	scan := testAsset("amb.jpg", 2009)
	g := ocrGroup{Cands: []workflows.OCRYearCandidate{{Asset: scan, Texts: []string{"1986 1993"}, Years: []int{1986, 1993}}}}
	r, _, calls := newTestReview("t\ny\n1993\nt\n", nil)
	if err := r.run([]ocrGroup{g}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].year != 1993 {
		t.Fatalf("calls = %+v, want one 1993 call", *calls)
	}

	r, _, calls = newTestReview("", nil) // closed stdin = quit, nothing changed
	if err := r.run([]ocrGroup{g}); err != nil || len(*calls) != 0 {
		t.Fatalf("EOF: err %v calls %+v", err, *calls)
	}
}

func TestWithoutMatching(t *testing.T) {
	change := workflows.OCRYearCandidate{Asset: testAsset("change.jpg", 2009), Years: []int{1986}}
	match := workflows.OCRYearCandidate{Asset: testAsset("match.jpg", 2004), Years: []int{2004}}
	amb := workflows.OCRYearCandidate{Asset: testAsset("amb.jpg", 2004), Years: []int{1986, 2004}}
	in := []workflows.OCRYearCandidate{change, match, amb}
	got := withoutMatching(in)
	if len(got) != 2 || got[0].Asset.OriginalFileName != "change.jpg" || got[1].Asset.OriginalFileName != "amb.jpg" {
		t.Fatalf("withoutMatching = %v", got)
	}
	if len(in) != 3 || in[1].Asset.OriginalFileName != "match.jpg" {
		t.Fatalf("input modified: %v", in)
	}
}

// [n] skips the remaining hits of the current album; the next album is
// still reviewed.
func TestOCRReviewNextAlbum(t *testing.T) {
	hit := func(name string) workflows.OCRYearCandidate {
		return workflows.OCRYearCandidate{Asset: testAsset(name, 2009), Texts: []string{"1986"}, Years: []int{1986}}
	}
	a1 := immichapi.AlbumResponseDto{Id: openapi_types.UUID(uuid.New()), AlbumName: "Familie"}
	a2 := immichapi.AlbumResponseDto{Id: openapi_types.UUID(uuid.New()), AlbumName: "Scans"}
	groups := []ocrGroup{
		{Album: &a1, Cands: []workflows.OCRYearCandidate{hit("f1.jpg"), hit("f2.jpg"), hit("f3.jpg")}},
		{Album: &a2, Cands: []workflows.OCRYearCandidate{hit("s1.jpg")}},
	}
	r, out, calls := newTestReview("n\nt\n", nil)
	if err := r.run(groups); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "f2.jpg") || strings.Contains(out.String(), "f3.jpg") {
		t.Errorf("skipped album hits were still prompted:\n%s", out)
	}
	if !strings.Contains(out.String(), "Skipping the remaining 3 hit(s)") {
		t.Errorf("no skip notice:\n%s", out)
	}
	if len(*calls) != 1 || strings.Join((*calls)[0].names, ",") != "s1.jpg" {
		t.Fatalf("calls = %+v, want only s1.jpg changed", *calls)
	}
}
