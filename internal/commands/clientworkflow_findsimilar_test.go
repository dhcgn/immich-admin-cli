package commands

import (
	"strings"
	"testing"
)

func TestFormatJSON(t *testing.T) {
	got := formatJSON([]byte(`{"duplicate":true,"matches":[{"assetId":"x"}]}`))
	if !strings.Contains(got, "\n") || !strings.Contains(got, `  "duplicate": true`) {
		t.Errorf("expected indented JSON, got:\n%s", got)
	}
	// Fallback: garbage in, same garbage out (no error, no empty output).
	if got := formatJSON([]byte(`not json`)); got != `not json` {
		t.Errorf("expected raw fallback, got %q", got)
	}
}
