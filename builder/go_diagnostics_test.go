package builder

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

const (
	mimetypeWarning        = "go: warning: github.com/gabriel-vasile/mimetype@v1.4.15: retracted by module author: quadratic time for specially crafted CDF files"
	conciseMimetypeWarning = "nflow: warning: github.com/gabriel-vasile/mimetype@v1.4.15 is retracted: quadratic time for specially crafted CDF files\n"
)

func TestGoWarningsDeduplicateWithinInvocation(t *testing.T) {
	ctx := WithGoDiagnostics(t.Context())
	var output bytes.Buffer
	block := "go: added example.com/module v1.0.0\n" + mimetypeWarning + "\n" +
		"go: to switch to the latest unretracted version, run:\n\tgo get github.com/gabriel-vasile/mimetype@latest\n"
	for range 3 {
		w := newGoWarningWriter(ctx, &output)
		// Exercise writes splitting both the warning prefix and module identity.
		for _, fragment := range []string{block[:5], block[5:50], block[50:]} {
			_, err := w.Write([]byte(fragment))
			ktest.RequireNoError(t, err)
		}
		w.Flush()
	}
	ktest.RequireEqual(t, output.String(), conciseMimetypeWarning)

	// A new invocation using the same parent must warn again, not share global state.
	next := newGoWarningWriter(WithGoDiagnostics(t.Context()), &output)
	_, err := next.Write([]byte(mimetypeWarning))
	ktest.RequireNoError(t, err)
	next.Flush() // Handles a final line without a newline.
	ktest.RequireEqual(t, output.String(), strings.Repeat(conciseMimetypeWarning, 2))
}

func TestGoWarningsConcurrentCommands(t *testing.T) {
	ctx := WithGoDiagnostics(t.Context())
	var output bytes.Buffer
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			w := newGoWarningWriter(ctx, &output)
			_, _ = w.Write([]byte(mimetypeWarning + "\n"))
			w.Flush()
		})
	}
	wg.Wait()
	ktest.RequireEqual(t, output.String(), conciseMimetypeWarning)
}

func TestGoWarningsRetainDistinctVersionsAndWarnings(t *testing.T) {
	var output bytes.Buffer
	w := newGoWarningWriter(WithGoDiagnostics(t.Context()), &output)
	_, err := fmt.Fprintln(w, mimetypeWarning)
	ktest.RequireNoError(t, err)
	_, err = fmt.Fprintln(w, strings.ReplaceAll(mimetypeWarning, "v1.4.15", "v1.4.14"))
	ktest.RequireNoError(t, err)
	_, err = fmt.Fprintln(w, "go: warning: another material warning")
	ktest.RequireNoError(t, err)
	w.Flush()
	ktest.RequireEqual(t, output.String(), conciseMimetypeWarning+
		strings.ReplaceAll(conciseMimetypeWarning, "v1.4.15", "v1.4.14")+
		"nflow: warning: another material warning\n")
}

func TestGoWarningsBoundLineMemoryWithoutLosingLaterWarnings(t *testing.T) {
	var output bytes.Buffer
	w := newGoWarningWriter(WithGoDiagnostics(t.Context()), &output)
	_, err := w.Write([]byte(strings.Repeat("x", 2*maxCapturedOutputBytes)))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(w.pending), maxCapturedOutputBytes)
	_, err = w.Write([]byte("\n" + mimetypeWarning + "\r\n"))
	ktest.RequireNoError(t, err)
	w.Flush()
	ktest.RequireEqual(t, output.String(), conciseMimetypeWarning)
}
