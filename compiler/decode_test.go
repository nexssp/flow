package compiler_test

import (
	"strconv"
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

func TestDecode_IntRangeUsesNativeWidth(t *testing.T) {
	t.Parallel()
	type cfg struct {
		Value int `nflow:"value"`
	}

	maxInt64 := int64((uint64(1) << (strconv.IntSize - 1)) - 1)
	minInt64 := -maxInt64 - 1
	for _, tc := range []struct {
		name string
		text string
		want int
	}{
		{name: "minimum", text: strconv.FormatInt(minInt64, 10), want: int(minInt64)},
		{name: "maximum", text: strconv.FormatInt(maxInt64, 10), want: int(maxInt64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compiler.Decode[cfg](map[string]string{"value": tc.text})
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got.Value != tc.want {
				t.Fatalf("Value = %d, want %d", got.Value, tc.want)
			}
		})
	}

	if strconv.IntSize == 64 {
		return // int64 has no representable decimal values outside native int here.
	}
	for _, tc := range []struct {
		name string
		text string
	}{
		{name: "below minimum", text: strconv.FormatInt(minInt64-1, 10)},
		{name: "above maximum", text: strconv.FormatInt(maxInt64+1, 10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := compiler.Decode[cfg](map[string]string{"value": tc.text}); err == nil {
				t.Fatalf("Decode accepted out-of-range int %q", tc.text)
			}
		})
	}
}

func TestDecode_Int64Retains64BitRange(t *testing.T) {
	t.Parallel()
	type cfg struct {
		Value int64 `nflow:"value"`
	}
	const text = "2147483648"
	got, err := compiler.Decode[cfg](map[string]string{"value": text})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Value != 2147483648 {
		t.Fatalf("Value = %d, want 2147483648", got.Value)
	}
}
