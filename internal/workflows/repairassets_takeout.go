package workflows

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// maxSidecarProbeBytes bounds how many bytes from the head of a file
// analyzeTakeoutSidecar reads and scans for the leading JSON object. Google
// Photos Takeout per-photo sidecars are a few hundred bytes to a few KB; 1 MiB
// is a generous ceiling that still refuses to treat an unbounded JSON-looking
// blob as a sidecar (if the object is not closed within the probe window it is
// not recognized).
const maxSidecarProbeBytes = 1 << 20

// takeoutTimestamp mirrors the { "timestamp": ..., "formatted": ... } objects
// used by both creationTime and photoTakenTime in a Google Photos Takeout
// sidecar. Only timestamp is needed for detection.
type takeoutTimestamp struct {
	Timestamp string `json:"timestamp"`
}

// takeoutSidecar mirrors the signature fields of a Google Photos Takeout
// per-photo metadata JSON. Pointer / RawMessage fields let detection tell
// "present" apart from "absent" so it can require a strong, specific
// combination of Google-only fields rather than trusting any single one.
type takeoutSidecar struct {
	Title              *string           `json:"title"`
	PhotoTakenTime     *takeoutTimestamp `json:"photoTakenTime"`
	CreationTime       *takeoutTimestamp `json:"creationTime"`
	GooglePhotosOrigin json.RawMessage   `json:"googlePhotosOrigin"`
}

// TakeoutSidecarAnalysis is the result of checking whether a file's bytes are
// actually a Google Photos Takeout metadata JSON sidecar that was imported in
// place of the real photo (a known Takeout export/import failure mode). Such a
// file contains no image data at all, so there is nothing to repair — the only
// safe action is to remove it.
type TakeoutSidecarAnalysis struct {
	// IsSidecar is true only when the file's leading bytes parse as a JSON
	// object carrying the full Google Takeout fingerprint (see
	// analyzeTakeoutSidecar). It is deliberately conservative: a false here on
	// a real sidecar merely means it won't be deleted, whereas a false-positive
	// would delete a real photo, so detection is biased hard against the latter.
	IsSidecar bool
	// Title is the original file name recorded inside the sidecar (e.g.
	// "IMG_1366.jpg"), surfaced purely for human-readable logging.
	Title string
	// JSONSize is the byte length of the leading JSON object.
	JSONSize int
}

// analyzeTakeoutSidecar reports whether the file at path is a Google Photos
// Takeout metadata JSON sidecar. Detection is structural, not a byte-prefix
// guess: it extracts the complete leading JSON object (brace-matched, string
// and escape aware), parses it, and requires ALL of a strong, Google-specific
// set of fields to be present — title, photoTakenTime.timestamp,
// creationTime.timestamp and googlePhotosOrigin. A real image never parses as
// a JSON object at all, and even an unrelated JSON document is extremely
// unlikely to carry that exact combination, so the false-positive risk (which
// would delete a real photo) is effectively nil.
func analyzeTakeoutSidecar(path string) (TakeoutSidecarAnalysis, error) {
	f, err := os.Open(path)
	if err != nil {
		return TakeoutSidecarAnalysis{}, err
	}
	defer f.Close()

	head := make([]byte, maxSidecarProbeBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return TakeoutSidecarAnalysis{}, err
	}
	return analyzeTakeoutSidecarBytes(head[:n]), nil
}

// analyzeTakeoutSidecarBytes applies the same structural Google Takeout
// fingerprint check as analyzeTakeoutSidecar to an in-memory head of a file.
// It is the shared core used both by the file-based probe and by the
// streaming head probe (which reads only the first maxSidecarProbeBytes of a
// remote asset, so an unsupported-extension sidecar can be recognized without
// downloading the whole file).
func analyzeTakeoutSidecarBytes(head []byte) TakeoutSidecarAnalysis {
	obj, ok := extractLeadingJSONObject(head)
	if !ok {
		return TakeoutSidecarAnalysis{} // does not start with a complete JSON object
	}

	var s takeoutSidecar
	dec := json.NewDecoder(bytes.NewReader(obj))
	if err := dec.Decode(&s); err != nil {
		return TakeoutSidecarAnalysis{} // not valid JSON → not a sidecar
	}

	if s.Title == nil || *s.Title == "" ||
		s.PhotoTakenTime == nil || s.PhotoTakenTime.Timestamp == "" ||
		s.CreationTime == nil || s.CreationTime.Timestamp == "" ||
		len(s.GooglePhotosOrigin) == 0 {
		return TakeoutSidecarAnalysis{} // missing part of the Google fingerprint
	}

	return TakeoutSidecarAnalysis{IsSidecar: true, Title: *s.Title, JSONSize: len(obj)}
}

// extractLeadingJSONObject returns the complete leading JSON object at the
// start of data (after an optional UTF-8 BOM and JSON whitespace), and true,
// or (nil, false) if data does not begin with an object that is fully closed
// within the buffer. The scan is string- and escape-aware so braces inside
// string values do not affect nesting depth; it stops at the matching close
// brace of the top-level object, ignoring any trailing (e.g. binary) bytes.
func extractLeadingJSONObject(data []byte) ([]byte, bool) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	i := 0
	for i < len(data) {
		if c := data[i]; c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		break
	}
	if i >= len(data) || data[i] != '{' {
		return nil, false
	}

	start := i
	depth := 0
	inStr := false
	esc := false
	for ; i < len(data); i++ {
		c := data[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return data[start : i+1], true
			}
		}
	}
	return nil, false // object never closed within the probe window
}
