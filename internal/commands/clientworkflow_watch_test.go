package commands

import (
	"testing"
	"time"
)

func TestParseIntervalOrOnce(t *testing.T) {
	d, once, err := parseIntervalOrOnce("60s", false)
	if err != nil || once || d != 60*time.Second {
		t.Fatalf("60s = %v,%v,%v; want 60s,false,nil", d, once, err)
	}
	if _, once, err := parseIntervalOrOnce("0s", false); err != nil || !once {
		t.Fatalf("0s should mean once: %v,%v", once, err)
	}
	if _, _, err := parseIntervalOrOnce("bogus", false); err == nil {
		t.Error("bogus interval expected error")
	}
}
