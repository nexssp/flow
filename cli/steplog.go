package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// StepLogger prints numbered build steps. In an interactive terminal it
// animates a spinner while a step is running. When stdout is redirected
// to a file or a pipe, the spinner is disabled so CI logs stay clean.
type StepLogger struct {
	w     io.Writer
	tty   bool
	total int

	mu      sync.Mutex
	current int
}

// NewStepLogger returns a logger that will print at most total steps.
func NewStepLogger(w io.Writer, total int) *StepLogger {
	if w == nil {
		w = io.Discard
	}
	return &StepLogger{w: w, tty: isTerminal(w), total: total}
}

// isTerminal reports whether w is an interactive character device.
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

// Step is one in-flight build step. Exactly one terminal method should
// be called; further calls are ignored.
type Step struct {
	logger *StepLogger
	index  int
	label  string
	detail string
	start  time.Time

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// Start begins a step. In a TTY it launches the spinner goroutine.
func (l *StepLogger) Start(label, detail string) *Step {
	l.mu.Lock()
	l.current++
	idx := l.current
	l.mu.Unlock()

	s := &Step{
		logger: l,
		index:  idx,
		label:  label,
		detail: detail,
		start:  time.Now(),
	}
	if l.tty {
		s.stop = make(chan struct{})
		s.done = make(chan struct{})
		go s.spin()
	}
	return s
}

// Done finishes the step with no suffix.
func (s *Step) Done() { s.finish("") }

// DoneTimed finishes the step and appends the elapsed wall clock.
func (s *Step) DoneTimed() {
	s.finish(fmt.Sprintf("(%.1fs)", time.Since(s.start).Seconds()))
}

// Fail finishes the step with a red error marker.
func (s *Step) Fail(err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	s.finish(failMarker + err.Error())
}

const failMarker = "\x00fail\x00"

func (s *Step) finish(suffix string) {
	s.once.Do(func() {
		if s.stop != nil {
			close(s.stop)
			<-s.done
		}
		l := s.logger
		if l.tty {
			_, _ = fmt.Fprint(l.w, "\r\x1b[K")
		}

		line := fmt.Sprintf("[%d/%d] %s %s",
			s.index, l.total,
			padRight(s.label, 14),
			s.detail,
		)
		switch {
		case suffix == "":
			// plain step, nothing to append
		case strings.HasPrefix(suffix, failMarker):
			line += "  " + paint("✗", ansiRed, true) + " " +
				strings.TrimPrefix(suffix, failMarker)
		default:
			line += "  " + suffix
		}
		fmt.Fprintln(l.w, line)
	})
}

// spin repaints the in-flight line until stop is closed.
func (s *Step) spin() {
	defer close(s.done)
	frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	render := func() {
		line := fmt.Sprintf("[%d/%d] %s %s  %c",
			s.index, s.logger.total,
			padRight(s.label, 14),
			s.detail,
			frames[i%len(frames)],
		)
		_, _ = fmt.Fprint(s.logger.w, "\r\x1b[K"+line)
		i++
	}
	render()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			render()
		}
	}
}
