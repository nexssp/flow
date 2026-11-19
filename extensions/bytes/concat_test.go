package bytes

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestConcatChunksPreservesEveryByteAndOrder(t *testing.T) {
	t.Parallel()

	first := make([]byte, 128)
	for i := range first {
		first[i] = byte(i)
	}
	second := []byte{0x00, 0xff, 0xfe, 'x'}
	third := make([]byte, 128)
	for i := range third {
		third[i] = byte(i + 128)
	}

	want := make([]byte, 0, len(first)+len(second)+len(third))
	want = append(want, first...)
	want = append(want, second...)
	want = append(want, third...)

	got, err := concatChunks(context.Background(), []any{first, second, []byte{}, third})
	if err != nil {
		t.Fatalf("concatChunks() error = %v", err)
	}
	if !bytes.Equal([]byte(got), want) {
		t.Fatalf("concatChunks() bytes = % x, want % x", []byte(got), want)
	}
}

func TestConcatChunksEmptyInput(t *testing.T) {
	t.Parallel()

	got, err := concatChunks(context.Background(), nil)
	if err != nil {
		t.Fatalf("concatChunks(nil) error = %v", err)
	}
	if got != "" {
		t.Fatalf("concatChunks(nil) = %q, want empty string", got)
	}
}

func TestConcatChunksRejectsNonByteValues(t *testing.T) {
	t.Parallel()

	_, err := concatChunks(context.Background(), []any{[]byte("ok"), "not bytes"})
	if err == nil || !strings.Contains(err.Error(), "chunk 1 must be []byte") {
		t.Fatalf("concatChunks() error = %v, want indexed []byte validation error", err)
	}
}
