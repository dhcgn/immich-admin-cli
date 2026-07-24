package workflows

import (
	"fmt"
	"io"
	"os"
)

// markerStrategy repairs JPEG files that are missing the mandatory
// End-of-Image marker (FF D9). It is append-only: the original bytes (and thus
// all EXIF metadata) are copied verbatim and the two missing marker bytes are
// appended, so nothing is re-encoded and no image data is lost.
type markerStrategy struct{}

func (markerStrategy) Name() string { return "marker" }

// Applicable reports that this strategy can repair files that are valid JPEGs
// at the start (have the SOI marker) but are missing the trailing EOI marker.
func (markerStrategy) Applicable(a JPEGAnalysis) bool {
	return a.HasSOI && !a.HasEOI
}

// Repair copies src to dst byte-for-byte and appends the End-of-Image marker
// FF D9.
func (markerStrategy) Repair(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating %q: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying image data: %w", err)
	}
	if _, err := out.Write([]byte{0xFF, 0xD9}); err != nil {
		return fmt.Errorf("appending EOI marker: %w", err)
	}
	return nil
}
