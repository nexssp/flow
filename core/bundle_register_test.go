package core

import "testing"

func TestRegister_Success(t *testing.T) {
	Register("test_register_success", func(_ map[string]string) Bundle {
		return Bundle{ID: "test_register_success"}
	})
	if _, ok := Lookup("test_register_success"); !ok {
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
	factory := func(_ map[string]string) Bundle {
		return Bundle{ID: "test_dup_internal"}
	}
	Register("test_dup_internal", factory)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate ID")
		}
	}()
	Register("test_dup_internal", factory)
}

func TestLookup_ExactID(t *testing.T) {
	Register("test_exact_internal", func(_ map[string]string) Bundle {
		return Bundle{ID: "test_exact_internal"}
	})
	if _, ok := Lookup("test_exact_internal"); !ok {
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
// the only contract RegisteredBundles documents.
func TestRegisteredBundles_Sorted(t *testing.T) {
	Register("zzz_sorted_probe", func(_ map[string]string) Bundle {
		return Bundle{ID: "zzz_sorted_probe"}
	})
	Register("aaa_sorted_probe", func(_ map[string]string) Bundle {
		return Bundle{ID: "aaa_sorted_probe"}
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
