package workflows

import (
	"strings"
	"testing"
)

// realTakeoutSidecarJSON is a representative Google Photos Takeout per-photo
// metadata sidecar (the exact shape observed in real corrupt imports), used to
// verify detection on the genuine fingerprint.
const realTakeoutSidecarJSON = `{
  "title": "IMG_1366.jpg",
  "description": "",
  "imageViews": "13",
  "creationTime": {
    "timestamp": "1551272434",
    "formatted": "Feb 27, 2019, 1:00:34 PM UTC"
  },
  "photoTakenTime": {
    "timestamp": "1542470225",
    "formatted": "Nov 17, 2018, 3:37:05 PM UTC"
  },
  "geoData": {
    "latitude": 0.0,
    "longitude": 0.0
  },
  "people": [{ "name": "Julia" }],
  "url": "https://photos.google.com/photo/abc",
  "googlePhotosOrigin": {
    "mobileUpload": { "deviceType": "ANDROID_PHONE" }
  }
}`

func TestAnalyzeTakeoutSidecar(t *testing.T) {
	t.Run("real sidecar followed by binary is detected", func(t *testing.T) {
		// Simulate the real corruption: the JSON sidecar written over the head
		// of the file, followed by leftover binary bytes.
		data := append([]byte(realTakeoutSidecarJSON), 0xFF, 0xD8, 0x00, 0x01, 0x7B, 0x7D, 0xE3, 0x50)
		p := writeTemp(t, "sidecar.jpg", data)
		a, err := analyzeTakeoutSidecar(p)
		if err != nil {
			t.Fatalf("analyzeTakeoutSidecar: %v", err)
		}
		if !a.IsSidecar {
			t.Fatal("IsSidecar = false, want true for a genuine Takeout sidecar")
		}
		if a.Title != "IMG_1366.jpg" {
			t.Errorf("Title = %q, want %q", a.Title, "IMG_1366.jpg")
		}
		if a.JSONSize != len(realTakeoutSidecarJSON) {
			t.Errorf("JSONSize = %d, want %d", a.JSONSize, len(realTakeoutSidecarJSON))
		}
	})

	t.Run("leading BOM and whitespace still detected", func(t *testing.T) {
		data := append([]byte{0xEF, 0xBB, 0xBF, '\n', ' ', '\t'}, []byte(realTakeoutSidecarJSON)...)
		p := writeTemp(t, "bom.jpg", data)
		a, err := analyzeTakeoutSidecar(p)
		if err != nil {
			t.Fatalf("analyzeTakeoutSidecar: %v", err)
		}
		if !a.IsSidecar {
			t.Error("IsSidecar = false, want true (BOM + whitespace before object)")
		}
	})

	t.Run("real JPEG is not a sidecar", func(t *testing.T) {
		data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0xFF, 0xD9}
		p := writeTemp(t, "photo.jpg", data)
		a, err := analyzeTakeoutSidecar(p)
		if err != nil {
			t.Fatalf("analyzeTakeoutSidecar: %v", err)
		}
		if a.IsSidecar {
			t.Error("IsSidecar = true, want false for a real JPEG")
		}
	})

	t.Run("unrelated JSON lacking the fingerprint is not a sidecar", func(t *testing.T) {
		// Valid JSON object, but not a Google Takeout sidecar.
		for _, js := range []string{
			`{"foo": "bar", "count": 3}`,
			// has title but nothing else
			`{"title": "x"}`,
			// missing creationTime + googlePhotosOrigin
			`{"title": "x", "photoTakenTime": {"timestamp": "1"}}`,
			// missing googlePhotosOrigin
			`{"title": "x", "photoTakenTime": {"timestamp": "1"}, "creationTime": {"timestamp": "2"}}`,
			// empty title
			`{"title": "", "photoTakenTime": {"timestamp": "1"}, "creationTime": {"timestamp": "2"}, "googlePhotosOrigin": {}}`,
		} {
			p := writeTemp(t, "other.json", []byte(js))
			a, err := analyzeTakeoutSidecar(p)
			if err != nil {
				t.Fatalf("analyzeTakeoutSidecar(%q): %v", js, err)
			}
			if a.IsSidecar {
				t.Errorf("IsSidecar = true for %q, want false", js)
			}
		}
	})

	t.Run("unterminated JSON object is not a sidecar", func(t *testing.T) {
		js := strings.TrimSuffix(realTakeoutSidecarJSON, "}") // drop the closing brace
		p := writeTemp(t, "trunc.jpg", []byte(js))
		a, err := analyzeTakeoutSidecar(p)
		if err != nil {
			t.Fatalf("analyzeTakeoutSidecar: %v", err)
		}
		if a.IsSidecar {
			t.Error("IsSidecar = true, want false for an unterminated JSON object")
		}
	})

	t.Run("empty file is not a sidecar", func(t *testing.T) {
		p := writeTemp(t, "empty.jpg", []byte{})
		a, err := analyzeTakeoutSidecar(p)
		if err != nil {
			t.Fatalf("analyzeTakeoutSidecar: %v", err)
		}
		if a.IsSidecar {
			t.Error("IsSidecar = true, want false for an empty file")
		}
	})
}

func TestExtractLeadingJSONObject(t *testing.T) {
	t.Run("braces inside strings do not affect nesting", func(t *testing.T) {
		obj, ok := extractLeadingJSONObject([]byte(`{"a": "}{ not real", "b": {"c": 1}}TRAILING`))
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := `{"a": "}{ not real", "b": {"c": 1}}`
		if string(obj) != want {
			t.Errorf("obj = %q, want %q", string(obj), want)
		}
	})

	t.Run("escaped quote inside string is handled", func(t *testing.T) {
		obj, ok := extractLeadingJSONObject([]byte(`{"a": "he said \"}\""}rest`))
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if string(obj) != `{"a": "he said \"}\""}` {
			t.Errorf("obj = %q", string(obj))
		}
	})

	t.Run("does not start with object", func(t *testing.T) {
		if _, ok := extractLeadingJSONObject([]byte("not json")); ok {
			t.Error("ok = true, want false")
		}
	})
}

func TestParseRepairModeTakeoutJSON(t *testing.T) {
	got, err := ParseRepairMode("takeout-json")
	if err != nil {
		t.Fatalf("ParseRepairMode(takeout-json): %v", err)
	}
	if got != RepairModeTakeoutJSON {
		t.Errorf("ParseRepairMode(takeout-json) = %q, want %q", got, RepairModeTakeoutJSON)
	}
}
