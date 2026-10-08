package cli

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// progressTracker renders a live status line while a long-running
// operation executes.
//
// On a TTY it overwrites a single line with a spinner and elapsed time,
// so a 2-minute cold `go mod tidy` or a blocked harness does not look
// like a hang.
//
// When stderr is redirected (CI, piped logs), startProgress is a no-op
// and only Complete emits a single result line. That keeps CI logs
// clean without per-request \r escapes.
type progressTracker struct {
	label    string
	start    time.Time
	tty      bool
	stop     chan struct{}
	done     chan struct{}
	complete sync.Once
}

// startProgress begins tracking. On a TTY the spinner starts
// immediately; on a redirected writer startProgress emits nothing.
func startProgress(label string) *progressTracker {
	p := &progressTracker{
		label: label,
		start: time.Now(),
		tty:   isTerminal(os.Stderr),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	if p.tty {
		go p.run()
	} else {
		close(p.done)
	}
	return p
}

func (p *progressTracker) run() {
	defer close(p.done)

	frames := [...]rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	render := func() {
		fmt.Fprintf(os.Stderr, "\r\x1b[K%s  %c  %s",
			p.label,
			frames[i%len(frames)],
			time.Since(p.start).Round(time.Millisecond))
		i++
	}
	render()

	for {
		select {
		case <-p.stop:
			fmt.Fprint(os.Stderr, "\r\x1b[K")
			return
		case <-ticker.C:
			render()
		}
	}
}

// Complete ends the tracker and prints "label  result (Xs)".
// Idempotent: only the first call has an effect. This makes it safe to
// call from both the first-output tap and the process-exit path — the
// first meaningful outcome wins.
func (p *progressTracker) Complete(result string) {
	p.complete.Do(func() {
		elapsed := time.Since(p.start).Round(time.Millisecond)
		if p.tty {
			close(p.stop)
			<-p.done
		}
		fmt.Fprintf(os.Stderr, "%s  %s (%s)\n", p.label, result, elapsed)
	})
}
