package workflows

import "strings"

// ExtensionForContentType maps an HTTP Content-Type (as returned when
// downloading a thumbnail/preview asset via GET /assets/{id}/thumbnail) to a
// filesystem extension, including the leading dot. Immich's thumbnail image
// format is a server-side configuration choice, not derived from the
// original asset's own file extension, so callers saving these binaries to
// disk must inspect the actual response Content-Type rather than assuming
// one. Unrecognized or missing content types fall back to ".jpg", Immich's
// most common thumbnail format.
func ExtensionForContentType(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch ct {
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	case "image/avif":
		return ".avif"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}
