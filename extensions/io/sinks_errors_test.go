package io

import (
	"errors"
	"iter"
	"strings"
	"testing"
)

var errSinkWrite = errors.New("sink write failed")

type sinkErrorWriter struct {
	err    error
	writes int
	output strings.Builder
}

func (w *sinkErrorWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.err != nil {
		return 0, w.err
	}
	return w.output.Write(p)
}

type sinkResult struct {
	item any
	err  error
}

func collectSinkResults(seq iter.Seq2[any, error]) []sinkResult {
	var got []sinkResult
	seq(func(item any, err error) bool {
		got = append(got, sinkResult{item: item, err: err})
		return true
	})
	return got
}

func TestWriteLines_HappyPath(t *testing.T) {
	writer := &sinkErrorWriter{}
	upstream := func(yield func(any, error) bool) {
		if !yield("first", nil) {
			return
		}
		yield("second", nil)
	}

	got := collectSinkResults(writeLines(upstream, writer, false))
	if len(got) != 2 || got[0] != (sinkResult{item: "first"}) || got[1] != (sinkResult{item: "second"}) {
		t.Fatalf("results = %#v, want two unchanged values", got)
	}
	if writer.output.String() != "first\nsecond\n" {
		t.Fatalf("output = %q, want %q", writer.output.String(), "first\nsecond\n")
	}
}

func TestWriteLines_PropagatesWriteError(t *testing.T) {
	writer := &sinkErrorWriter{err: errSinkWrite}
	upstream := func(yield func(any, error) bool) {
		yield(strings.Repeat("x", 8192), nil)
	}

	got := collectSinkResults(writeLines(upstream, writer, false))
	if len(got) != 1 || !errors.Is(got[0].err, errSinkWrite) {
		t.Fatalf("results = %#v, want one write error", got)
	}
	if writer.writes == 0 {
		t.Fatal("underlying writer was not called")
	}
}

func TestWriteLines_PropagatesTerminalFlushError(t *testing.T) {
	writer := &sinkErrorWriter{err: errSinkWrite}
	upstream := func(yield func(any, error) bool) {
		yield("line", nil)
	}

	got := collectSinkResults(writeLines(upstream, writer, true))
	if len(got) != 1 || !errors.Is(got[0].err, errSinkWrite) {
		t.Fatalf("results = %#v, want one terminal flush error", got)
	}
	if writer.writes == 0 {
		t.Fatal("terminal flush did not reach the underlying writer")
	}
}

func TestWriteLines_PropagatesFinalFlushError(t *testing.T) {
	writer := &sinkErrorWriter{err: errSinkWrite}
	upstream := func(yield func(any, error) bool) {
		yield("line", nil)
	}

	got := collectSinkResults(writeLines(upstream, writer, false))
	if len(got) != 2 || got[0] != (sinkResult{item: "line"}) || !errors.Is(got[1].err, errSinkWrite) {
		t.Fatalf("results = %#v, want the value followed by one final flush error", got)
	}
}

func TestWriteLines_UpstreamErrorTakesPrecedenceOverCleanupFlushError(t *testing.T) {
	writer := &sinkErrorWriter{err: errSinkWrite}
	upstream := func(yield func(any, error) bool) {
		if !yield("buffered", nil) {
			return
		}
		yield(nil, errUpstream)
	}

	got := collectSinkResults(writeLines(upstream, writer, false))
	if len(got) != 2 || got[0] != (sinkResult{item: "buffered"}) || !errors.Is(got[1].err, errUpstream) {
		t.Fatalf("results = %#v, want the item followed by the upstream error", got)
	}
	if errors.Is(got[1].err, errSinkWrite) || writer.writes != 1 {
		t.Fatalf("cleanup flush was not best-effort or replaced the upstream error: results=%#v writes=%d", got, writer.writes)
	}
}

func TestWriteLines_EarlyStopDoesNotYieldCleanupFlushError(t *testing.T) {
	for _, flushEach := range []bool{false, true} {
		for _, failCleanup := range []bool{false, true} {
			writer := &sinkErrorWriter{}
			produced := 0
			upstream := func(yield func(any, error) bool) {
				for _, item := range []string{"first", "second"} {
					produced++
					if !yield(item, nil) {
						return
					}
				}
			}
			var got []sinkResult
			writeLines(upstream, writer, flushEach)(func(item any, err error) bool {
				got = append(got, sinkResult{item: item, err: err})
				if failCleanup {
					writer.err = errSinkWrite
				}
				return false
			})

			if len(got) != 1 || got[0] != (sinkResult{item: "first"}) || produced != 1 || writer.writes != 1 {
				t.Fatalf("flushEach=%v failCleanup=%v: results=%#v produced=%d writes=%d, want one item and one write", flushEach, failCleanup, got, produced, writer.writes)
			}
		}
	}
}
