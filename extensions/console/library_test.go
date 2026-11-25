package console

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/nexssp/kernel/xerr"
)

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

func TestStderrClassifiesUnavailableWriter(t *testing.T) {
	_, err := Library().Actions[0].DoAny(context.Background(), StderrRequest{Message: "hello"})
	if got := xerr.KindFrom(err); got != xerr.KindUnavailable {
		t.Fatalf("missing writer error kind = %q, want %q: %v", got, xerr.KindUnavailable, err)
	}
}

func TestStderrClassifiesWriteFailureAndPreservesCause(t *testing.T) {
	_, err := Library().Actions[0].DoAny(
		WithStderrWriter(context.Background(), failingWriter{err: io.ErrClosedPipe}),
		StderrRequest{Message: "hello"},
	)
	if got := xerr.KindFrom(err); got != xerr.KindUnavailable {
		t.Fatalf("write error kind = %q, want %q: %v", got, xerr.KindUnavailable, err)
	}
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error = %v, want wrapped %v cause", err, io.ErrClosedPipe)
	}
}
