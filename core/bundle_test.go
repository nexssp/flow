package core_test

import (
	"strings"
	"testing"

	"github.com/nexssp/flow/core"
)

// ── Register: input validation ─────────────────────────────────────

func TestRegister_RejectsNilFactory(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil factory")
		}
	}()
	core.Register("test_nil_factory_ext", nil)
}

func TestRegister_RejectsEmptyID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty ID")
		}
	}()
	core.Register("", func(map[string]string) core.Bundle { return core.Bundle{} })
}

func TestRegister_RejectsWhitespaceID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on whitespace-only ID")
		}
	}()
	core.Register("   ", func(map[string]string) core.Bundle { return core.Bundle{} })
}

func TestRegister_DuplicatePanics(t *testing.T) {
	factory := func(map[string]string) core.Bundle { return core.Bundle{} }
	core.Register("test_dup_external", factory)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "duplicate") {
			t.Fatalf("panic message = %v, want substring %q", r, "duplicate")
		}
	}()
	core.Register("test_dup_external", factory)
}

// ── Lookup: resolution rules ───────────────────────────────────────

func TestLookup_ExactID(t *testing.T) {
	core.Register("test_exact_external", func(map[string]string) core.Bundle {
		return core.Bundle{ID: "test_exact_external"}
	})
	if _, ok := core.LookupBundleForModule("test_exact_external"); !ok {
		t.Fatal("exact ID lookup failed")
	}
}

func TestLookup_ModulePathVariants(t *testing.T) {
	core.Register("test_ai_external", func(map[string]string) core.Bundle {
		return core.Bundle{ID: "test_ai_external"}
	})

	cases := []struct {
		query string
		want  bool
	}{
		{"test_ai_external", true},
		{"github.com/nexssp/test_ai_external", true},
		{"github.com/nexssp/test_ai_external/nexssflow", true},
		{"github.com/nexssp/test_ai_external/nexssflow/", true},
		{"github.com/acme/test_ai_external", true},
		{"github.com/acme/test_ai_external/nexssflow", true},
		{"github.com/any/nested/path/test_ai_external", true},
		{"github.com/nexssp/test_ai_external2", false},
		{"test_ai_external2", false},
		{"", false},
		{"/", false},
		{"//", false},
	}

	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			_, ok := core.LookupBundleForModule(c.query)
			if ok != c.want {
				t.Fatalf("LookupBundleForModule(%q) = %v, want %v", c.query, ok, c.want)
			}
		})
	}
}

func TestLookup_ForkSafe(t *testing.T) {
	core.Register("test_macros_external", func(map[string]string) core.Bundle {
		return core.Bundle{ID: "test_macros_external"}
	})

	for _, target := range []string{
		"github.com/nexssp/flow/extensions/test_macros_external",
		"github.com/acme/flow/extensions/test_macros_external",
		"github.com/acme/flow/extensions/test_macros_external/nexssflow",
	} {
		if _, ok := core.LookupBundleForModule(target); !ok {
			t.Errorf("target %q did not resolve", target)
		}
	}
}

func TestLookup_Unknown(t *testing.T) {
	if _, ok := core.LookupBundleForModule("github.com/nonexistent/package"); ok {
		t.Error("unknown module path should not resolve")
	}
}

func TestLookup_NormalizationEdges(t *testing.T) {
	core.Register("test_edge_external", func(map[string]string) core.Bundle {
		return core.Bundle{ID: "test_edge_external"}
	})

	cases := []struct {
		name  string
		query string
		want  bool
	}{
		{"bare ID", "test_edge_external", true},
		{"module path", "github.com/x/test_edge_external", true},
		{"nexssflow marker", "github.com/x/test_edge_external/nexssflow", true},
		{"trailing slash", "github.com/x/test_edge_external/nexssflow/", true},
		{"windows backslash", `github.com\x\test_edge_external\nexssflow`, true},
		{"windows local path", `.\custom\test_edge_external`, true},
		{"empty", "", false},
		{"slash only", "/", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok := core.LookupBundleForModule(c.query)
			if ok != c.want {
				t.Fatalf("LookupBundleForModule(%q) = %v, want %v", c.query, ok, c.want)
			}
		})
	}
}
