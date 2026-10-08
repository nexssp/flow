package io

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

var errUpstream = errors.New("upstream failed")

func TestWriteLineTo_PassesThrough(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	op := writeLineTo(func() io.Writer { return &buf })

	upstream := func(yield func(any, error) bool) {
		yield("one", nil)
		yield("two", nil)
	}

	var got []any
	for item, err := range op(upstream) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, item)
	}

	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("got %#v", got)
	}
	if buf.String() != "one\ntwo\n" {
		t.Fatalf("sink output = %q", buf.String())
	}
}

func TestWriteLineTo_PropagatesUpstreamError(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	op := writeLineTo(func() io.Writer { return &buf })

	upstream := func(yield func(any, error) bool) {
		yield("partial", nil)
		yield(nil, errUpstream)
	}

	var sawErr error
	for _, err := range op(upstream) {
		if err != nil {
			sawErr = err
		}
	}
	if !errors.Is(sawErr, errUpstream) {
		t.Fatalf("err = %v, want errUpstream", sawErr)
	}
	if buf.String() != "partial\n" {
		t.Fatalf("sink output = %q", buf.String())
	}
}

func TestBundle_WiresExpectedAtoms(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)

	if bundle.ID != ID {
		t.Fatalf("ID = %q", bundle.ID)
	}
	if len(bundle.Libraries) != 1 {
		t.Fatalf("libraries = %d", len(bundle.Libraries))
	}

	lib := bundle.Libraries[0]
	if len(lib.Sources) != 1 || lib.Sources[0].Describe().Name != "io.stdin" {
		t.Fatalf("source wiring: %#v", lib.Sources)
	}

	names := make(map[string]bool, len(lib.Operators))
	for _, op := range lib.Operators {
		names[op.Name] = true
	}
	for _, want := range []string{"io.stdout", "io.stderr"} {
		if !names[want] {
			t.Fatalf("missing operator %q", want)
		}
	}
}

func TestStdoutOperator_ResolvesStdoutAtDrainTime(t *testing.T) {
	// Not parallel: reassigns os.Stdout process-wide.

	op := StdoutOperator()
	built, err := op.Build(nil)
	if err != nil {
		t.Fatal(err)
	}

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })

	upstream := func(yield func(any, error) bool) {
		yield("captured", nil)
	}

	stream, err := built.Apply(upstream)
	if err != nil {
		t.Fatal(err)
	}
	var seen []any
	stream(func(item any, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, item)
		return true
	})

	_ = writer.Close()
	captured, _ := io.ReadAll(reader)
	_ = reader.Close()

	if string(captured) != "captured\n" {
		t.Fatalf("stdout = %q, want %q", captured, "captured\n")
	}
	if len(seen) != 1 || seen[0] != "captured" {
		t.Fatalf("passthrough = %#v", seen)
	}
}
