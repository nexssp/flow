package io

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type cancelAfterRead struct {
	reader *strings.Reader
	cancel context.CancelFunc
}

func (r *cancelAfterRead) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.cancel()
	return n, err
}

func TestInFrom_Lines(t *testing.T) {
	t.Parallel()

	source := inFrom(strings.NewReader("alpha\nbeta\ngamma\n"))
	seq, err := source.Do(context.Background(), InConfig{Format: "lines"})
	if err != nil {
		t.Fatal(err)
	}

	var got []any
	for item, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, item)
	}

	want := []any{"alpha", "beta", "gamma"}
	if len(got) != len(want) || got[0] != want[0] || got[2] != want[2] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestInFrom_DefaultFormatIsLines(t *testing.T) {
	t.Parallel()

	source := inFrom(strings.NewReader("x\n"))
	seq, err := source.Do(context.Background(), InConfig{})
	if err != nil {
		t.Fatal(err)
	}

	for item := range seq {
		if item != "x" {
			t.Fatalf("got %v", item)
		}
	}
}

func TestInFrom_NDJSON(t *testing.T) {
	t.Parallel()

	body := `{"id":1,"name":"a"}` + "\n" + `{"id":2,"name":"b"}` + "\n"
	source := inFrom(strings.NewReader(body))
	seq, err := source.Do(context.Background(), InConfig{Format: "ndjson"})
	if err != nil {
		t.Fatal(err)
	}

	var got []any
	for item, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, item)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	first, ok := got[0].(map[string]any)
	if !ok || first["name"] != "a" {
		t.Fatalf("unexpected first item: %#v", got[0])
	}
}

func TestInFrom_NDJSON_SkipsEmptyLines(t *testing.T) {
	t.Parallel()

	source := inFrom(strings.NewReader("\n\n" + `{"k":1}` + "\n\n"))
	seq, err := source.Do(context.Background(), InConfig{Format: "ndjson"})
	if err != nil {
		t.Fatal(err)
	}

	var count int
	for range seq {
		count++
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestInFrom_UnknownFormat(t *testing.T) {
	t.Parallel()

	source := inFrom(strings.NewReader(""))
	_, err := source.Do(context.Background(), InConfig{Format: "csv"})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestInFrom_CanceledContextStopsBeforeScanning(t *testing.T) {
	for _, format := range []string{"lines", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			source := inFrom(strings.NewReader("payload\n"))
			seq, err := source.Do(ctx, InConfig{Format: format})
			if err != nil {
				t.Fatalf("create source sequence: %v", err)
			}
			var yieldedValue bool
			var yieldedErr error
			for _, itemErr := range seq {
				if itemErr != nil {
					yieldedErr = itemErr
					continue
				}
				yieldedValue = true
			}
			if yieldedValue {
				t.Fatal("canceled source yielded a value")
			}
			if !errors.Is(yieldedErr, context.Canceled) {
				t.Fatalf("source error = %v, want context.Canceled", yieldedErr)
			}
		})
	}
}

func TestInFrom_CancellationAfterScanSuppressesValue(t *testing.T) {
	for _, format := range []string{"lines", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := inFrom(&cancelAfterRead{reader: strings.NewReader("payload\n"), cancel: cancel})
			seq, err := source.Do(ctx, InConfig{Format: format})
			if err != nil {
				t.Fatalf("create source sequence: %v", err)
			}
			var yieldedValue bool
			var yieldedErr error
			for _, itemErr := range seq {
				if itemErr != nil {
					yieldedErr = itemErr
					continue
				}
				yieldedValue = true
			}
			if yieldedValue {
				t.Fatal("canceled source yielded a value")
			}
			if !errors.Is(yieldedErr, context.Canceled) {
				t.Fatalf("source error = %v, want context.Canceled", yieldedErr)
			}
		})
	}
}

func TestInFrom_AllowsLineAtSupportedLimit(t *testing.T) {
	cases := []struct {
		format     string
		line       string
		wantLength int
	}{
		{format: "lines", line: strings.Repeat("x", scanMaxLineBytes), wantLength: scanMaxLineBytes},
		{format: "ndjson", line: `"` + strings.Repeat("x", scanMaxLineBytes-2) + `"`, wantLength: scanMaxLineBytes - 2},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			source := inFrom(strings.NewReader(tc.line + "\r\n"))
			seq, err := source.Do(context.Background(), InConfig{Format: tc.format})
			if err != nil {
				t.Fatalf("create source sequence: %v", err)
			}
			count := 0
			for item, itemErr := range seq {
				if itemErr != nil {
					t.Fatalf("read line: %v", itemErr)
				}
				value, ok := item.(string)
				if !ok || len(value) != tc.wantLength {
					t.Fatalf("item = %#v, want string of %d bytes", item, tc.wantLength)
				}
				count++
			}
			if count != 1 {
				t.Fatalf("yielded %d items, want 1", count)
			}
		})
	}
}

func TestInFrom_RejectsLineAboveSupportedLimit(t *testing.T) {
	line := strings.Repeat("x", scanMaxLineBytes+1)
	for _, format := range []string{"lines", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			source := inFrom(strings.NewReader(line))
			seq, err := source.Do(context.Background(), InConfig{Format: format})
			if err != nil {
				t.Fatalf("create source sequence: %v", err)
			}
			var gotErr error
			for _, itemErr := range seq {
				if itemErr != nil {
					gotErr = itemErr
				}
			}
			if gotErr == nil {
				t.Fatal("oversized line produced no error")
			}
		})
	}
}
