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

func TestEffectiveStableFor(t *testing.T) {
	if d, err := effectiveStableFor("10s", false); err != nil || d != 10*time.Second {
		t.Fatalf("10s,false = %v,%v; want 10s,nil", d, err)
	}
	// --once forces immediate upload regardless of the flag value.
	if d, err := effectiveStableFor("10s", true); err != nil || d != 0 {
		t.Fatalf("10s,true = %v,%v; want 0,nil", d, err)
	}
	if _, err := effectiveStableFor("bogus", false); err == nil {
		t.Error("bogus stable-for expected error")
	}
	if _, err := effectiveStableFor("-1s", false); err == nil {
		t.Error("negative stable-for expected error")
	}
}
