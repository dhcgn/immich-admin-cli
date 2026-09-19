package clipprobe

import (
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptionsValidate(t *testing.T) {
	if err := DefaultOptions().Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	for _, tc := range []Options{
		{Limit: 0, MaxDistance: 0.01, Type: "IMAGE"},
		{Limit: 101, MaxDistance: 0.01, Type: "IMAGE"},
		{Limit: 10, MaxDistance: -0.1, Type: "IMAGE"},
		{Limit: 10, MaxDistance: 2.1, Type: "IMAGE"},
		{Limit: 10, MaxDistance: 0.01, Type: "BOGUS"},
	} {
		if err := tc.Validate(); err == nil {
			t.Errorf("expected error for %+v", tc)
		}
	}
}

func TestFindSimilarRoundtrip(t *testing.T) {
	var gotKey, gotQuery, gotFileName string
	var gotFileBytes []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotQuery = r.URL.RawQuery
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parsing multipart: %v", err)
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("missing file part: %v", err)
		} else {
			gotFileName = hdr.Filename
			buf := make([]byte, 4)
			n, _ := f.Read(buf)
			gotFileBytes = buf[:n]
			f.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SimilarResponse{
			Duplicate:   true,
			MaxDistance: 0.01,
			Query:       Query{FileName: "q.jpg", Bytes: 4, Width: 2, Height: 2, Model: "ViT-B-32__openai", Dimensions: 512},
			Matches:     []Match{{AssetID: "0421250e-23a9-4a5b-a9ac-23c9bd7cafdb", OriginalFileName: "DSC_2031.jpg", Type: "IMAGE", Distance: 0.0028, Similarity: 0.9971}},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	img := filepath.Join(dir, "q.jpg")
	if err := os.WriteFile(img, []byte{0xFF, 0xD8, 0xFF, 0xD9}, 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(srv.URL, "sekret")
	resp, raw, err := c.FindSimilar(context.Background(), img, DefaultOptions())
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if gotKey != "sekret" {
		t.Errorf("x-api-key = %q", gotKey)
	}
	for _, want := range []string{"limit=10", "max_distance=0.01", "type=IMAGE"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if gotFileName != "q.jpg" {
		t.Errorf("filename = %q", gotFileName)
	}
	if len(gotFileBytes) != 4 {
		t.Errorf("file bytes = %d", len(gotFileBytes))
	}
	if !resp.Duplicate || len(resp.Matches) != 1 || resp.Matches[0].AssetID != "0421250e-23a9-4a5b-a9ac-23c9bd7cafdb" {
		t.Errorf("unexpected response: %+v", resp)
	}
	var check SimilarResponse
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Errorf("raw body is not JSON: %v", err)
	}
}

func TestFindSimilarErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = multipart.NewWriter(nil)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","message":"bad token"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	img := filepath.Join(dir, "q.jpg")
	if err := os.WriteFile(img, []byte{1, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := New(srv.URL, "wrong").FindSimilar(context.Background(), img, DefaultOptions())
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}
