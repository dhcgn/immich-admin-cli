// Package workflows: per-file byte progress for uploads/downloads.
// stdlib-only counting io.Reader wrapper reporting to stderr.
package workflows

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ByteProgress tracks one file transfer, printing a single-line bar to
// stderr (stdout stays clean for --json/piping).
// Total < 0 means unknown (chunked response): bytes+rate only, no %.
type ByteProgress struct {
	Label string
	Total int64
	Done  int64
	Start time.Time
	Pos   int
	N     int
	Quiet bool
	Out   io.Writer

	tty   bool
	last  time.Time
	final bool
}

// NewByteProgress creates a transfer tracker. total < 0 = unknown length.
// quiet disables all output (--quiet, or --json which implies quiet).
func NewByteProgress(label string, total int64, pos, n int, quiet bool) *ByteProgress {
	p := &ByteProgress{
		Label: label,
		Total: total,
		Start: time.Now(),
		Pos:   pos,
		N:     n,
		Quiet: quiet,
		Out:   os.Stderr,
	}
	p.tty = isTerminalWriter(p.Out)
	return p
}

// Wrap returns r wrapped so every Read counts toward the bar.
func (p *ByteProgress) Wrap(r io.Reader) io.Reader {
	if p == nil {
		return r
	}
	return &countingReader{r: r, p: p}
}

// Add counts n bytes and prints a throttled update.
func (p *ByteProgress) Add(n int64) {
	if p == nil || p.Quiet || n <= 0 {
		if p != nil {
			p.Done += max64(0, n)
		}
		return
	}
	p.Done += n
	now := time.Now()
	complete := p.Total >= 0 && p.Done >= p.Total
	if !complete && !p.last.IsZero() && now.Sub(p.last) < 200*time.Millisecond {
		return
	}
	p.last = now
	p.print(false)
}

// Finish prints the final state (full bar) and ends the TTY line.
func (p *ByteProgress) Finish() {
	if p == nil || p.Quiet || p.final {
		return
	}
	p.final = true
	p.print(true)
}

func (p *ByteProgress) print(final bool) {
	if p.Out == nil {
		p.Out = os.Stderr
	}
	line := FormatByteProgressLine(p.Pos, p.N, p.Label, p.Done, p.Total, time.Since(p.Start))
	if p.tty {
		// Live in-place single line (TTY only; ANSI clear is fine here).
		fmt.Fprintf(p.Out, "\r%s\x1b[K", line)
		if final {
			fmt.Fprintln(p.Out)
		}
		return
	}
	fmt.Fprintln(p.Out, line)
}

// FormatByteProgressLine renders one progress line. Pure for testing.
func FormatByteProgressLine(pos, n int, label string, done, total int64, elapsed time.Duration) string {
	head := fmt.Sprintf("[%d/%d] %s %s", pos, n, label, formatBytesShort(done))
	rate := formatRate(done, elapsed)
	if total < 0 {
		return fmt.Sprintf("%s [%s]", head, rate)
	}
	if total == 0 {
		return fmt.Sprintf("%s 100%% [%s] %s", head, strings.Repeat("█", 12), rate)
	}
	pct := float64(done) / float64(total) * 100
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * 12)
	bar := strings.Repeat("█", filled) + strings.Repeat("-", 12-filled)
	return fmt.Sprintf("%s %s %.0f%% [%s] %s", head, formatBytesShort(total), pct, bar, rate)
}

func formatBytesShort(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func formatRate(done int64, elapsed time.Duration) string {
	if elapsed <= 0 {
		return formatBytesShort(done) + "/s"
	}
	perSec := float64(done) / elapsed.Seconds()
	return formatBytesShort(int64(perSec)) + "/s"
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

type countingReader struct {
	r io.Reader
	p *ByteProgress
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	if n > 0 {
		c.p.Add(int64(n))
	}
	return n, err
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}
