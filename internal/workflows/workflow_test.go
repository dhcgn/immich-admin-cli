package workflows

import (
	"strings"
	"testing"
	"time"
)

func TestProgressLine(t *testing.T) {
	tests := []struct {
		name         string
		position     int
		total        int
		elapsed      time.Duration
		label        string
		wantContains []string
		wantNoETA    bool
	}{
		{
			name:         "first item: no eta yet",
			position:     1,
			total:        10,
			elapsed:      2 * time.Second,
			label:        "IMG_0001.jpg",
			wantContains: []string{"[1/10", "10.0%", "elapsed 2s", "IMG_0001.jpg"},
			wantNoETA:    true,
		},
		{
			name:         "second item: eta derived from first item's duration",
			position:     2,
			total:        10,
			elapsed:      10 * time.Second, // item 1 took 10s
			label:        "IMG_0002.jpg",
			wantContains: []string{"[2/10", "20.0%", "elapsed 10s", "eta 1m30s", "IMG_0002.jpg"},
		},
		{
			name:         "last item: 100 percent, eta reflects the one item still to run",
			position:     10,
			total:        10,
			elapsed:      100 * time.Second,
			label:        "IMG_0010.jpg",
			wantContains: []string{"[10/10", "100.0%", "elapsed 1m40s", "eta 11s", "IMG_0010.jpg"},
		},
		{
			name:         "zero total does not panic or divide by zero",
			position:     1,
			total:        0,
			elapsed:      time.Second,
			label:        "whatever",
			wantContains: []string{"[1/0", "whatever"},
			wantNoETA:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := progressLine(tc.position, tc.total, tc.elapsed, tc.label)
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("progressLine(%d, %d, %s, %q) = %q, want it to contain %q", tc.position, tc.total, tc.elapsed, tc.label, got, want)
				}
			}
			if tc.wantNoETA && strings.Contains(got, "eta") {
				t.Errorf("progressLine(%d, %d, %s, %q) = %q, want no eta yet", tc.position, tc.total, tc.elapsed, tc.label, got)
			}
		})
	}
}

func TestProgressStepAdvancesPosition(t *testing.T) {
	p := NewProgress(3)
	if p.done != 0 {
		t.Fatalf("NewProgress().done = %d, want 0", p.done)
	}
	p.Step("a")
	if p.done != 1 {
		t.Errorf("after first Step(), done = %d, want 1", p.done)
	}
	p.Step("b")
	if p.done != 2 {
		t.Errorf("after second Step(), done = %d, want 2", p.done)
	}
}
