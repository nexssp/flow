package io

import (
	"context"
	"strings"
	"testing"
)

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
