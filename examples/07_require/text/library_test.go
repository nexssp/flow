package text

import (
	"context"
	"testing"

	"github.com/nexssp/testkit"
)

// Pure function test. Fast, no action wrapper.
func TestUppercaseFunction(t *testing.T) {
	res, err := uppercase(context.Background(), UppercaseReq{Message: "hi"})
	if err != nil {
		t.Fatal(err)
	}

	if res.Message != "HI" {
		t.Fatalf("got %q, want HI", res.Message)
	}
}

// Typed action test. Uses the same builder the library wraps.
func TestUppercaseAction(t *testing.T) {
	res, err := Uppercase().Do(context.Background(), UppercaseReq{Message: "hi"})
	if err != nil {
		t.Fatal(err)
	}

	if res.Message != "HI" {
		t.Fatalf("got %q, want HI", res.Message)
	}
}

// Metadata contract. Runs the whole library through the shared
// invariant suite: names, aliases, hooks, payload shape.
func TestLibraryContract(t *testing.T) {
	lib := Library()
	if lib.Name == "" {
		t.Fatal("Library() must set Name")
	}

	if len(lib.Actions) == 0 {
		t.Fatal("Library() must return at least one action")
	}

	testkit.AssertContracts(t, lib.Actions)
}

// Benchmark with testkit. Works because Uppercase() returns a typed
// *action.BuiltAction, exactly what testkit.BenchAction expects.
func BenchmarkUppercase(b *testing.B) {
	testkit.BenchAction(b, Uppercase(), UppercaseReq{Message: "hi"})
}

// Concurrency test with testkit. Same reason.
func TestUppercaseConcurrent(t *testing.T) {
	testkit.Simulate(t, Uppercase(), UppercaseReq{Message: "hi"}, 20,
		func(t testing.TB, res UppercaseRes, err error) {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if res.Message != "HI" {
				t.Errorf("got %q, want HI", res.Message)
			}
		})
}
