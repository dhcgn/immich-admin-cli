// Package workflows implements client-side multi-step orchestrations
// ("client workflows") that combine several Immich API calls with local
// processing into one command. They run entirely client-side and are the
// main purpose of this tool — not to be confused with Immich's server-side
// Workflows API.
//
// A workflow is an ordered list of named Steps executed for one asset (or
// asset pair); RunSteps handles step logging, --dry-run, and stopping at the
// first failed step so later steps (in particular the always-last
// destructive step) never run after an earlier failure. RunBatch layers the
// same continue-on-error + summary-exit-code convention used by bulk
// commands (see internal/commands/helpers.go) on top, for running a workflow
// across many items. Progress reports "[i/N] elapsed/eta" lines for
// long-running batches (e.g. download-album's --resize/
// --resize-video-preset re-encoding) so users can gauge how far along a
// multi-minute run is and roughly how much longer it will take.
package workflows

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Step is a single named operation within a workflow, executed for one item
// (e.g. one asset or asset pair).
type Step struct {
	// Name is a short human-readable description shown in --dry-run output
	// and progress logging (e.g. "Upload new file").
	Name string
	// Run performs the step. A non-nil error aborts the remaining steps for
	// this item.
	Run func(ctx context.Context) error
}

// RunOptions controls how RunSteps executes a step list.
type RunOptions struct {
	// DryRun, when true, prints the planned steps without calling any Run
	// function.
	DryRun bool
}

// RunSteps executes steps in order for one item identified by label.
//
// In dry-run mode it only prints the planned steps and returns nil.
// Normally it runs each step in order, printing a line as each completes,
// and stops at the first failing step — the failure is wrapped with the
// step name and label and returned immediately, so no later step (in
// particular a destructive step placed last) ever runs after an earlier one
// failed.
func RunSteps(ctx context.Context, opts RunOptions, label string, steps []Step) error {
	if opts.DryRun {
		fmt.Printf("[dry-run] %s: would run %d step(s):\n", label, len(steps))
		for i, s := range steps {
			fmt.Printf("[dry-run]   %d) %s\n", i+1, s.Name)
		}
		return nil
	}

	for _, s := range steps {
		if err := s.Run(ctx); err != nil {
			return fmt.Errorf("%s: step %q failed: %w", label, s.Name, err)
		}
		fmt.Printf("%s: %s... ok\n", label, s.Name)
	}
	return nil
}

// RunBatch runs fn(item) for every item, continuing on error. Failures are
// logged to stderr via label(item); the batch always continues to the next
// item. It returns a summary error if any item failed, or nil if all
// succeeded — the same convention used by the bulk commands in
// internal/commands (e.g. assetsInfo, assetsDownloadOriginal).
func RunBatch[T any](items []T, label func(T) string, fn func(T) error) error {
	failures := 0
	for _, item := range items {
		if err := fn(item); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", label(item), err)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d item(s) failed", failures, len(items))
	}
	return nil
}

// Progress prints a "[i/total  P%] elapsed .., eta ..: label" line to
// stderr before each item of a long-running batch starts, so a slow
// multi-minute run (e.g. downloading and re-encoding many large photos and
// videos) gives the user a running sense of how far along it is and
// roughly how much longer it will take, instead of going silent until it
// finishes. It is not a generic progress-bar library — just enough for
// this project's sequential, one-item-at-a-time batches (see RunBatch);
// concurrent use from multiple goroutines is not supported.
type Progress struct {
	total int
	done  int
	start time.Time
}

// NewProgress creates a Progress tracker for a batch of total items, with
// its elapsed-time clock starting now.
func NewProgress(total int) *Progress {
	return &Progress{total: total, start: time.Now()}
}

// Step prints the progress line for the next item (labeled, e.g., by its
// file name) and advances the internal counter. The ETA shown is a simple
// linear extrapolation from the average time per item completed so far; it
// is omitted before the second item, since one data point can't be
// averaged into a rate yet.
func (p *Progress) Step(label string) {
	p.done++
	fmt.Fprintln(os.Stderr, progressLine(p.done, p.total, time.Since(p.start), label))
}

// progressLine renders the text for one Progress.Step call, given the
// item's 1-based position among total and the time elapsed since the
// batch started. Pure (no time.Now() call, no I/O) so it is directly
// unit-testable.
func progressLine(position, total int, elapsed time.Duration, label string) string {
	pct := 100.0
	if total > 0 {
		pct = float64(position) / float64(total) * 100
	}
	line := fmt.Sprintf("[%d/%d %5.1f%%] elapsed %s", position, total, pct, elapsed.Round(time.Second))

	completed := position - 1
	if completed > 0 {
		avgPerItem := elapsed / time.Duration(completed)
		remaining := avgPerItem * time.Duration(total-completed)
		line += fmt.Sprintf(", eta %s", remaining.Round(time.Second))
	}
	return line + ": " + label
}
