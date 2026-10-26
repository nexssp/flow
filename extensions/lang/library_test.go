package lang

import (
	"errors"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestBundleContractAndRunnerExports(t *testing.T) {
	bundle := Bundle(nil)
	if err := compiler.ValidateBundle(bundle); err != nil {
		t.Fatalf("ValidateBundle: %v", err)
	}
	if len(bundle.ExportedModules) != 8 {
		t.Fatalf("ExportedModules = %d; want 8", len(bundle.ExportedModules))
	}
	for _, name := range bundle.ExportedModules {
		if _, err := bundle.SourceModules.Open(name); err != nil {
			t.Errorf("exported runner %q is unavailable: %v", name, err)
		}
	}
}

// TestRegisterRejectsDuplicateID ensures Register panics on duplicate IDs.
// The project rule: "collisions panic; missing contracts fail loudly."
func TestRegisterRejectsDuplicateID(t *testing.T) {
	// Save and restore Registry so this test is isolated.
	saved := Registry["python"]
	defer func() { Registry["python"] = saved }()

	delete(Registry, "python")
	Register(Language{ID: "python", Ext: ".py", Select: func(string) (string, []string, error) { return "", nil, ErrNotFound }})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with duplicate ID did not panic")
		}
	}()
	Register(Language{ID: "python", Ext: ".py", Select: func(string) (string, []string, error) { return "", nil, ErrNotFound }})
}

// TestRegisterRejectsEmptyID ensures Register panics on empty ID.
func TestRegisterRejectsEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with empty ID did not panic")
		}
	}()
	Register(Language{ID: ""})
}

// TestLookupMiss ensures Lookup returns an error for unregistered IDs.
func TestLookupMiss(t *testing.T) {
	_, err := Lookup("nonexistent", "")
	if err == nil {
		t.Fatal("Lookup for nonexistent ID returned nil error")
	}
	e, ok := errors.AsType[*UnknownLanguageError](err)
	if !ok || e.ID != "nonexistent" {
		t.Fatalf("Lookup error type unexpected: %T %v", err, err)
	}
}

// TestCodecs exercises the JSON and text codecs directly. These are the
// shared boundary across all languages; if they regress, every language does.
func TestCodecs(t *testing.T) {
	t.Run("encodeJSON nil", func(t *testing.T) {
		b, err := encodeJSON(nil)
		if err != nil {
			t.Fatal(err)
		}
		if b != nil {
			t.Errorf("encodeJSON(nil) = %v; want nil", b)
		}
	})
	t.Run("encodeJSON string", func(t *testing.T) {
		b, err := encodeJSON("hello")
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != `"hello"` {
			t.Errorf(`encodeJSON("hello") = %q; want %q`, b, `"hello"`)
		}
	})
	t.Run("decodeJSON empty", func(t *testing.T) {
		v, err := decodeJSON("")
		if err != nil {
			t.Fatal(err)
		}
		if v != nil {
			t.Errorf("decodeJSON(\"\") = %v; want nil", v)
		}
	})
	t.Run("encodeText string", func(t *testing.T) {
		b, err := encodeText("raw")
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "raw" {
			t.Errorf("encodeText = %q; want %q", b, "raw")
		}
	})
	t.Run("encodeText fallback to JSON", func(t *testing.T) {
		b, err := encodeText(map[string]int{"x": 1})
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != `{"x":1}` {
			t.Errorf("encodeText fallback = %q; want %q", b, `{"x":1}`)
		}
	})
	t.Run("decodeText", func(t *testing.T) {
		v, err := decodeText("hello")
		if err != nil {
			t.Fatal(err)
		}
		if v != "hello" {
			t.Errorf("decodeText = %v; want %q", v, "hello")
		}
	})
}
