package compiler_test

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow/compiler"
)

var bundleTestSequence atomic.Uint64

func bundleTestID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, bundleTestSequence.Add(1))
}

func TestRegister_RejectsNilFactory(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil factory")
		}
	}()
	compiler.Register("test_nil_factory_ext", nil)
}

func TestRegister_RejectsEmptyID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty ID")
		}
	}()
	compiler.Register("", func(map[string]string) compiler.Bundle { return compiler.Bundle{} })
}

func TestRegister_RejectsWhitespaceID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on whitespace-only ID")
		}
	}()
	compiler.Register("   ", func(map[string]string) compiler.Bundle { return compiler.Bundle{} })
}

func TestRegister_DuplicatePanics(t *testing.T) {
	id := bundleTestID("test_dup_external")
	factory := func(map[string]string) compiler.Bundle { return compiler.Bundle{ID: id} }
	compiler.Register(id, factory)

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
	compiler.Register(id, factory)
}

func TestLookup_ExactID(t *testing.T) {
	id := bundleTestID("test_exact_external")
	compiler.Register(id, func(map[string]string) compiler.Bundle {
		return compiler.Bundle{ID: id}
	})
	if _, ok := compiler.LookupBundleForModule(id); !ok {
		t.Fatal("exact ID lookup failed")
	}
}

func TestLookup_ModulePathVariants(t *testing.T) {
	id := bundleTestID("test_module_external")
	compiler.Register(id, func(map[string]string) compiler.Bundle {
		return compiler.Bundle{ID: id}
	})

	cases := []struct {
		query string
		want  bool
	}{
		{id, true},
		{"github.com/nexssp/" + id, true},
		{fmt.Sprintf("github.com/nexssp/%s/nexssflow", id), true},
		{fmt.Sprintf("github.com/nexssp/%s/nexssflow/", id), true},
		{"github.com/nexssp/" + id, true},
		{fmt.Sprintf("github.com/nexssp/%s/nexssflow", id), true},
		{"github.com/any/nested/path/" + id, true},
		{fmt.Sprintf("github.com/nexssp/%s2", id), false},
		{id + "2", false},
		{"", false},
		{"/", false},
		{"//", false},
	}

	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			_, ok := compiler.LookupBundleForModule(c.query)
			if ok != c.want {
				t.Fatalf("LookupBundleForModule(%q) = %v, want %v", c.query, ok, c.want)
			}
		})
	}
}

func TestLookup_ForkSafe(t *testing.T) {
	id := bundleTestID("test_macros_external")
	compiler.Register(id, func(map[string]string) compiler.Bundle {
		return compiler.Bundle{ID: id}
	})

	for _, target := range []string{
		"github.com/nexssp/flow/extensions/" + id,
		"github.com/nexssp/flow/extensions/" + id,
		fmt.Sprintf("github.com/nexssp/flow/extensions/%s/nexssflow", id),
	} {
		if _, ok := compiler.LookupBundleForModule(target); !ok {
			t.Errorf("target %q did not resolve", target)
		}
	}
}

func TestLookup_Unknown(t *testing.T) {
	if _, ok := compiler.LookupBundleForModule("github.com/nonexistent/package"); ok {
		t.Error("unknown module path should not resolve")
	}
}

func TestLookup_NormalizationEdges(t *testing.T) {
	id := bundleTestID("test_edge_external")
	compiler.Register(id, func(map[string]string) compiler.Bundle {
		return compiler.Bundle{ID: id}
	})

	cases := []struct {
		name  string
		query string
		want  bool
	}{
		{"bare ID", id, true},
		{"module path", "github.com/x/" + id, true},
		{"nexssflow marker", fmt.Sprintf("github.com/x/%s/nexssflow", id), true},
		{"trailing slash", fmt.Sprintf("github.com/x/%s/nexssflow/", id), true},
		{"windows backslash", fmt.Sprintf(`github.com\x\%s\nexssflow`, id), true},
		{"windows local path", `.\custom\` + id, true},
		{"empty", "", false},
		{"slash only", "/", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok := compiler.LookupBundleForModule(c.query)
			if ok != c.want {
				t.Fatalf("LookupBundleForModule(%q) = %v, want %v", c.query, ok, c.want)
			}
		})
	}
}
