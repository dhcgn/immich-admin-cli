package workflows

import "testing"

func TestExtensionForContentType(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
	}{
		{"image/webp", ".webp"},
		{"image/jpeg", ".jpg"},
		{"image/jpeg; charset=binary", ".jpg"},
		{"image/png", ".png"},
		{"image/avif", ".avif"},
		{"image/gif", ".gif"},
		{"application/octet-stream", ".jpg"},
		{"", ".jpg"},
		{"IMAGE/WEBP", ".webp"},
	}

	for _, tc := range tests {
		if got := ExtensionForContentType(tc.contentType); got != tc.want {
			t.Errorf("ExtensionForContentType(%q) = %q, want %q", tc.contentType, got, tc.want)
		}
	}
}
