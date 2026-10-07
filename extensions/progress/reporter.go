package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/nexssp/kernel/xctx"
)

// Reporter renders xctx.Progress events to a writer. It is safe for
// concurrent use and safe to Shutdown more than once.
type Reporter struct {
	out io.Writer
	tty bool

	mu     sync.Mutex
	active *activeStep
	closed bool
}

type activeStep struct {
	message string
	start   time.Time
	stop    chan struct{}
	done    chan struct{}
}

// NewReporter returns a Reporter writing to out. A nil writer falls
// back to os.Stderr. The Reporter detects TTY-ness once at construction.
func NewReporter(out io.Writer) *Reporter {
	if out == nil {
		out = os.Stderr
	}
	return &Reporter{out: out, tty: isTerminal(out)}
}

// Report implements xctx.ProgressReporter.
//
// Events are dispatched on Metadata["kind"]:
//
//	"step"   — one-shot progress line; current/total are ignored
//	"wrap"   — lifecycle event; Current==0 starts, Current>=Total ends
//
// A bare Progress with a Message and no kind is rendered as a step, so
// callers who publish xctx.Progress directly get visible output without
// having to know this package's metadata convention.
func (r *Reporter) Report(p xctx.Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}

	kind, _ := p.Metadata["kind"].(string)
	switch kind {
	case "wrap":
		r.handleWrapLocked(p)
	default:
		if p.Message != "" {
			r.printStepLocked(p.Message)
		}
	}
}

// Shutdown stops any active spinner. Safe to call more than once.
func (r *Reporter) Shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.stopSpinnerLocked()
}

// ── step rendering ───────────────────────────────────────────────────

func (r *Reporter) printStepLocked(message string) {
	if !r.tty {
		fmt.Fprintf(r.out, "✓ %s\n", message)
		return
	}
	fmt.Fprintf(r.out, "\x1b[32m✓\x1b[0m %s\n", message)
}

// ── wrap lifecycle ───────────────────────────────────────────────────

func (r *Reporter) handleWrapLocked(p xctx.Progress) {
	if p.Message == "" {
		return
	}
	isStart := p.Total > 0 && p.Current == 0
	isDone := p.Total > 0 && p.Current >= p.Total

	switch {
	case isStart:
		r.stopSpinnerLocked()
		r.startSpinnerLocked(p.Message)
	case isDone:
		_, failed := p.Metadata["error"]
		r.finishSpinnerLocked(p.Message, failed)
	}
}

func (r *Reporter) startSpinnerLocked(message string) {
	if !r.tty {
		fmt.Fprintf(r.out, "▶ %s\n", message)
		return
	}
	s := &activeStep{
		message: message,
		start:   time.Now(),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	r.active = s
	go r.spin(s)
}

func (r *Reporter) finishSpinnerLocked(message string, failed bool) {
	s := r.active
	r.active = nil

	if s == nil {
		r.printStepLockedWithStatus(message, failed)
		return
	}

	close(s.stop)
	<-s.done

	elapsed := time.Since(s.start).Round(time.Millisecond)
	if !r.tty {
		marker := "✓"
		if failed {
			marker = "✗"
		}
		fmt.Fprintf(r.out, "%s %s  (%s)\n", marker, message, elapsed)
		return
	}

	marker := "\x1b[32m✓\x1b[0m"
	if failed {
		marker = "\x1b[31m✗\x1b[0m"
	}
	fmt.Fprintf(r.out, "\r\x1b[K%s %s  \x1b[2m(%s)\x1b[0m\n", marker, message, elapsed)
}

func (r *Reporter) printStepLockedWithStatus(message string, failed bool) {
	if !r.tty {
		marker := "✓"
		if failed {
			marker = "✗"
		}
		fmt.Fprintf(r.out, "%s %s\n", marker, message)
		return
	}
	marker := "\x1b[32m✓\x1b[0m"
	if failed {
		marker = "\x1b[31m✗\x1b[0m"
	}
	fmt.Fprintf(r.out, "%s %s\n", marker, message)
}

func (r *Reporter) stopSpinnerLocked() {
	s := r.active
	r.active = nil
	if s == nil {
		return
	}
	close(s.stop)
	<-s.done
}

func (r *Reporter) spin(s *activeStep) {
	defer close(s.done)
	frames := [...]rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	render := func() {
		elapsed := time.Since(s.start).Round(time.Millisecond)
		fmt.Fprintf(r.out, "\r\x1b[K%s  %c  \x1b[2m%s\x1b[0m",
			s.message, frames[i%len(frames)], elapsed)
		i++
	}
	render()

	for {
		select {
		case <-s.stop:
			fmt.Fprint(r.out, "\r\x1b[K")
			return
		case <-ticker.C:
			render()
		}
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
