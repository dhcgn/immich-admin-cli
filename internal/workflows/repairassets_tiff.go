package workflows

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// maxTIFFIFDs bounds how many IFDs the chain walk in analyzeTIFF will follow,
// guarding against pathological/looping input.
const maxTIFFIFDs = 1000

// maxTIFFIFDEntries bounds how many entries a single IFD may declare before
// analyzeTIFF gives up on it as implausible (the TIFF 6.0 spec has no hard
// limit, but tens of thousands of entries in one IFD never occurs in real
// files and is a sign the offset we're reading isn't actually an IFD).
const maxTIFFIFDEntries = 4096

// TIFFZeroCountTag records one IFD entry whose 4-byte "count" field is
// literally 0. Every TIFF field must have at least one value (TIFF 6.0 §2),
// so count==0 is unconditionally invalid — this is exactly the condition
// libtiff's _TIFFVSetField rejects fatally ("Null count for Tag N"), which is
// the confirmed root cause of Immich's "Input file has corrupt header"
// thumbnail failures for this defect (validated against real libtiff 4.5.1).
type TIFFZeroCountTag struct {
	// Tag is the field tag ID (e.g. 0x8657).
	Tag uint16
	// Type is the field's declared TIFF data type (e.g. 1=BYTE, 2=ASCII).
	Type uint16
	// CountFieldOffset is the absolute byte offset, within the file, of the
	// 4-byte count field to patch during repair.
	CountFieldOffset int64
}

// TIFFAnalysis is the result of structurally walking a file's TIFF IFD chain
// to look for the zero-count defect. It deliberately does NOT attempt a full
// image decode (no golang.org/x/image/tiff): a raw IFD walk works for any
// TIFF variant/compression/bit-depth libtiff itself would accept, whereas a
// decode-based check would only work for the narrow subset of TIFFs Go's
// image libraries can decode.
type TIFFAnalysis struct {
	// Valid reports whether the file has a recognizable TIFF header (II/MM +
	// magic 42) and every IFD in the main chain (IFD0, IFD1, ...) could be
	// walked without a structural read error. This does NOT claim the file is
	// otherwise undamaged — only that the walk that produced ZeroCountTags
	// below is trustworthy. If Valid is false, ZeroCountTags is always empty:
	// we do not report a defect we can't be sure about.
	Valid bool
	// ZeroCountTags lists every zero-count IFD entry found while walking the
	// main IFD chain. A strategy is Applicable only when this is non-empty —
	// i.e. only when the specific, verified defect was actually found, never
	// as a blanket "this is a TIFF" heuristic.
	ZeroCountTags []TIFFZeroCountTag
}

// analyzeTIFF walks the TIFF IFD chain (IFD0, IFD1, ...) of the file at path
// looking for entries with a zero count field. It only walks the main IFD
// chain reachable from the header's first-IFD offset (not EXIF/GPS/private
// sub-IFDs reached via tag pointers): that is where this defect has been
// observed in practice, and restricting the walk to offsets whose structure
// we can independently verify (rather than following arbitrary tag values
// into what might be pixel data) avoids false positives.
//
// Any structural surprise (bad header, unreadable/implausible IFD, a chain
// that doesn't terminate) causes analyzeTIFF to return Valid=false with no
// tags reported, rather than guessing — Applicable() then correctly reports
// false, so no repair is attempted on a file we couldn't confidently parse.
func analyzeTIFF(path string) (TIFFAnalysis, error) {
	f, err := os.Open(path)
	if err != nil {
		return TIFFAnalysis{}, err
	}
	defer f.Close()

	header := make([]byte, 8)
	if _, err := io.ReadFull(f, header); err != nil {
		return TIFFAnalysis{}, nil // too short to be a TIFF; not an I/O failure
	}

	var order binary.ByteOrder
	switch string(header[0:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return TIFFAnalysis{}, nil // not a TIFF header we recognize
	}
	if order.Uint16(header[2:4]) != 42 {
		return TIFFAnalysis{}, nil // wrong magic number
	}

	var tags []TIFFZeroCountTag
	visited := make(map[int64]bool)
	next := int64(order.Uint32(header[4:8]))

	for i := 0; next != 0; i++ {
		if i >= maxTIFFIFDs || visited[next] || next < 0 {
			return TIFFAnalysis{}, nil // implausible chain length or a loop
		}
		visited[next] = true

		countBuf := make([]byte, 2)
		if _, err := f.ReadAt(countBuf, next); err != nil {
			return TIFFAnalysis{}, nil
		}
		entryCount := int(order.Uint16(countBuf))
		if entryCount == 0 || entryCount > maxTIFFIFDEntries {
			return TIFFAnalysis{}, nil
		}

		entriesBuf := make([]byte, entryCount*12)
		if _, err := f.ReadAt(entriesBuf, next+2); err != nil {
			return TIFFAnalysis{}, nil
		}
		for e := range entryCount {
			entry := entriesBuf[e*12 : e*12+12]
			count := order.Uint32(entry[4:8])
			if count == 0 {
				tags = append(tags, TIFFZeroCountTag{
					Tag:              order.Uint16(entry[0:2]),
					Type:             order.Uint16(entry[2:4]),
					CountFieldOffset: next + 2 + int64(e*12) + 4,
				})
			}
		}

		nextBuf := make([]byte, 4)
		if _, err := f.ReadAt(nextBuf, next+2+int64(entryCount*12)); err != nil {
			return TIFFAnalysis{}, nil
		}
		next = int64(order.Uint32(nextBuf))
	}

	return TIFFAnalysis{Valid: true, ZeroCountTags: tags}, nil
}

// tiffZeroCountStrategy repairs the zero-count IFD tag defect by patching
// each offending count field to 1, byte-for-byte, in an otherwise untouched
// copy of the file. This is layout-preserving (no bytes shift, file size is
// unchanged) and needs no image decode: it works for any TIFF compression or
// bit depth, since it never interprets pixel data.
type tiffZeroCountStrategy struct{}

func (tiffZeroCountStrategy) Name() string { return "tiff-zero-count" }

// Applicable reports true only when analyzeTIFF successfully walked the IFD
// chain (Valid) AND found at least one entry with the exact zero-count
// defect. It is never true merely because a file is a TIFF.
func (tiffZeroCountStrategy) Applicable(a TIFFAnalysis) bool {
	return a.Valid && len(a.ZeroCountTags) > 0
}

func (tiffZeroCountStrategy) Repair(src, dst string) error {
	analysis, err := analyzeTIFF(src)
	if err != nil {
		return fmt.Errorf("re-analysing %q: %w", src, err)
	}
	if !analysis.Valid || len(analysis.ZeroCountTags) == 0 {
		return fmt.Errorf("tiff-zero-count: no zero-count IFD tags found in %q", src)
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copying %q to %q: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("closing %q: %w", dst, err)
	}

	header := make([]byte, 2)
	hf, err := os.Open(dst)
	if err != nil {
		return err
	}
	_, err = io.ReadFull(hf, header)
	hf.Close()
	if err != nil {
		return fmt.Errorf("reading header of %q: %w", dst, err)
	}
	var order binary.ByteOrder
	if string(header) == "II" {
		order = binary.LittleEndian
	} else {
		order = binary.BigEndian
	}

	f, err := os.OpenFile(dst, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	one := make([]byte, 4)
	order.PutUint32(one, 1)
	for _, t := range analysis.ZeroCountTags {
		if _, err := f.WriteAt(one, t.CountFieldOffset); err != nil {
			return fmt.Errorf("patching tag %#04x at offset %d: %w", t.Tag, t.CountFieldOffset, err)
		}
	}
	return nil
}
