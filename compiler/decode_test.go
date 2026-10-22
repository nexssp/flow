package compiler_test

import (
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestDecode_NflowTag(t *testing.T) {
	type cfg struct {
		Addr string `nflow:"addr" default:":8090"`
		Name string `nflow:"name"`
	}
	got, err := compiler.Decode[cfg](map[string]string{"name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Addr != ":8090" || got.Name != "x" {
		t.Fatalf("tags not read: %+v", got)
	}
}

func TestDecode_ExplicitEmptyStringDoesNotUseDefault(t *testing.T) {
	t.Parallel()
	type cfg struct {
		Name string `nflow:"name" default:"fallback"`
	}
	got, err := compiler.Decode[cfg](map[string]string{"name": ""})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "" {
		t.Fatalf("Name = %q, want explicit empty value", got.Name)
	}
	got, err = compiler.Decode[cfg](nil)
	if err != nil || got.Name != "fallback" {
		t.Fatalf("missing option = %+v, %v; want default", got, err)
	}
}

func TestDecode_RejectsNonStructTargets(t *testing.T) {
	t.Parallel()
	if _, err := compiler.Decode[string](nil); err == nil {
		t.Fatal("Decode[string] unexpectedly succeeded")
	}
}
