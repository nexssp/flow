package text

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
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

// flow-capability contract, not ingress; ktest.AssertContracts is service-oriented.
// Evaluates Flow invariants for capabilities: ensuring metadata is present.
func TestLibraryContract(t *testing.T) {
	lib := Library()
	if lib.Name == "" {
		t.Fatal("Library() must set Name")
	}

	if len(lib.Actions) == 0 {
		t.Fatal("Library() must return at least one action")
	}

	for _, act := range lib.Actions {
		meta := act.Describe()
		if meta.Name == "" {
			t.Errorf("Action missing Name")
		}
		if meta.Description == "" {
			t.Errorf("Action %q missing Description", meta.Name)
		}
		if len(meta.Tags) == 0 {
			t.Errorf("Action %q missing Tags", meta.Name)
		}
	}
}

// Benchmark with testkit. Works because Uppercase() returns a typed
// *action.BuiltAction, exactly what testkit.BenchAction expects.
func BenchmarkUppercase(b *testing.B) {
	ktest.BenchAction(b, Uppercase(), UppercaseReq{Message: "hi"})
}

// Concurrency test with testkit. Same reason.
func TestUppercaseConcurrent(t *testing.T) {
	ktest.Simulate(t, Uppercase(), UppercaseReq{Message: "hi"}, 20,
		func(t testing.TB, res UppercaseRes, err error) {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if res.Message != "HI" {
				t.Errorf("got %q, want HI", res.Message)
			}
		})
}
