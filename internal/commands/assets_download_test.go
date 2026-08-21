package commands

import (
	"testing"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func TestResolveDownloadThumbnailSize(t *testing.T) {
	tests := []struct {
		raw     string
		want    immichapi.AssetMediaSize
		wantErr bool
	}{
		{raw: "fullsize", want: immichapi.Fullsize},
		{raw: "preview", want: immichapi.Preview},
		{raw: "thumbnail", want: immichapi.Thumbnail},
		// "original" is deprecated on GET /assets/{id}/thumbnail per the
		// OpenAPI spec and must be rejected here (use download-original).
		{raw: "original", wantErr: true},
		{raw: "", wantErr: true},
		{raw: "bogus", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := resolveDownloadThumbnailSize(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("resolveDownloadThumbnailSize(%q) error = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("resolveDownloadThumbnailSize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
