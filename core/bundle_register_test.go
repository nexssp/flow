package core

import (
	"fmt"
	"sync/atomic"
	"testing"
)

var bundleTestSequence atomic.Uint64

func bundleTestID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, bundleTestSequence.Add(1))
}

func TestRegister_Success(t *testing.T) {
	id := bundleTestID("test_register_success")
	Register(id, func(_ map[string]string) Bundle {
		return Bundle{ID: id}
	})
	if _, ok := Lookup(id); !ok {
		t.Fatal("registered bundle not found")
	}
}

func TestRegister_PanicsOnNilFactory(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil factory")
		}
	}()
	Register("test_nil_factory", nil)
}

func TestRegister_PanicsOnEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty ID")
		}
	}()
	Register("", func(_ map[string]string) Bundle { return Bundle{ID: "x"} })
}

func TestRegister_PanicsOnWhitespaceID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on whitespace-only ID")
		}
	}()
	Register("   ", func(_ map[string]string) Bundle { return Bundle{ID: "x"} })
}

func TestRegister_PanicsOnDuplicateID(t *testing.T) {
	id := bundleTestID("test_dup_internal")
	factory := func(_ map[string]string) Bundle {
		return Bundle{ID: id}
	}
	Register(id, factory)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate ID")
		}
	}()
	Register(id, factory)
}

func TestLookup_ExactID(t *testing.T) {
	id := bundleTestID("test_exact_internal")
	Register(id, func(_ map[string]string) Bundle {
		return Bundle{ID: id}
	})
	if _, ok := Lookup(id); !ok {
		t.Fatal("exact ID lookup failed")
	}
}

func TestLookup_Unknown(t *testing.T) {
	if _, ok := Lookup("nonexistent_bundle_xyz"); ok {
		t.Error("unknown bundle should not resolve")
	}
}

// TestRegisteredBundles_Sorted cannot assert the exact number of
// bundles in the registry because other tests in this binary add
// their own. It registers two probe bundles whose IDs bookend the
// alphabet and asserts that the returned slice is sorted, which is
// the only contract RegisteredBundles documents. Unique IDs keep repeated
// -count runs independent even though registrations live for the process.
func TestRegisteredBundles_Sorted(t *testing.T) {
	lastID := bundleTestID("zzz_sorted_probe")
	Register(lastID, func(_ map[string]string) Bundle {
		return Bundle{ID: lastID}
	})
	firstID := bundleTestID("aaa_sorted_probe")
	Register(firstID, func(_ map[string]string) Bundle {
		return Bundle{ID: firstID}
	})

	bundles := RegisteredBundles()
	if len(bundles) < 2 {
		t.Fatalf("expected at least 2 bundles, got %d", len(bundles))
	}
	for i := 1; i < len(bundles); i++ {
		if bundles[i-1].ID >= bundles[i].ID {
			t.Fatalf("bundles not sorted: %q >= %q", bundles[i-1].ID, bundles[i].ID)
		}
	}
}
