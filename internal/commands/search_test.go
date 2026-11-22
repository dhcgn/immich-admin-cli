package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/config"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// runSearchFlags parses args with the real `search metadata` flags and
// returns the built request body.
func runSearchFlags(t *testing.T, args ...string) (*immichapi.MetadataSearchDto, error) {
	t.Helper()
	meta := Search().Commands[0]
	var body *immichapi.MetadataSearchDto
	var buildErr error
	cmd := &cli.Command{
		Name:  "metadata",
		Flags: meta.Flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			body, buildErr = buildMetadataSearchDto(cmd)
			return nil
		},
	}
	if err := cmd.Run(context.Background(), append([]string{"metadata"}, args...)); err != nil {
		t.Fatalf("parsing flags: %v", err)
	}
	return body, buildErr
}

// No deprecated flat field may be set: the server rejects mixing them with
// filter/orderBy (400 "Deprecated field … cannot be combined with orderBy").
func TestBuildMetadataSearchDtoUsesOnlyStructuredShape(t *testing.T) {
	body, err := runSearchFlags(t,
		"--original-file-name", "IMG", "--ocr", "1986", "--type", "IMAGE", "--make", "Canon",
		"--is-not-in-album", "true", "--is-favorite", "false",
		"--taken-after", "2024-01-01T00:00:00Z", "--taken-before", "2024-12-31T23:59:59Z", "--order", "asc")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	for k := range top {
		if k != "filter" && k != "orderBy" && k != "size" {
			t.Errorf("unexpected top-level field %q in %s", k, raw)
		}
	}
	for _, want := range []string{
		`"originalFileName":{"like":"IMG"}`, `"ocr":{"matches":"1986"}`, `"type":{"eq":"IMAGE"}`,
		`"make":{"eq":"Canon"}`, `"hasAlbums":{"eq":false}`, `"isFavorite":{"eq":false}`,
		`"takenAt":{"gte":"2024-01-01T00:00:00Z","lte":"2024-12-31T23:59:59Z"}`, `"orderBy":{"direction":"asc"}`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("body lacks %s:\n%s", want, raw)
		}
	}
}

func TestBuildMetadataSearchDtoRejectsInvalidEnums(t *testing.T) {
	for _, args := range [][]string{{"--type", "image"}, {"--order", "up"}, {"--taken-after", "2024-01-01"}} {
		if _, err := runSearchFlags(t, args...); err == nil {
			t.Errorf("%v: expected error", args)
		}
	}
}

func TestSearchExcludingTrashSendsExplicitNull(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"albums":{"items":[],"count":0,"total":0,"facets":[]},"assets":{"items":[],"count":0,"total":0,"facets":[]}}`))
	}))
	defer srv.Close()
	c, err := client.New(&config.Config{Server: srv.URL, APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ocr := immichapi.SearchFilter{Ocr: &immichapi.StringSimilarityFilter{Matches: "1986"}}
	if _, err := workflows.SearchExcludingTrash(context.Background(), c, immichapi.MetadataSearchDto{Filter: &ocr}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"trashedAt":{"eq":null}`) || !strings.Contains(got, `"ocr":{"matches":"1986"}`) {
		t.Errorf("request body = %s", got)
	}
}
