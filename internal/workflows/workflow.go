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
// across many items.
package workflows

import (
	"context"
	"fmt"
	"os"
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
