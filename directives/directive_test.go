package directives

import (
	"strings"
	"testing"
)

func TestLookup_BoundaryRule(t *testing.T) {
	cases := []struct {
		line string
		want string
		ok   bool
	}{
		{"@profile: trusted", "profile", true},
		{"@profile", "profile", true},
		{"@pipeline foo", "pipeline", true},
		{"@profilefoo", "", false},
		{"profile: trusted", "", false},
		{"", "", false},
		{"@", "", false},
	}
	for _, c := range cases {
		d, ok := Lookup(c.line)
		if ok != c.ok {
			t.Errorf("%q: ok = %v, want %v", c.line, ok, c.ok)
			continue
		}
		if ok && d.Name() != c.want {
			t.Errorf("%q: got %q, want %q", c.line, d.Name(), c.want)
		}
	}
}

func TestLookup_RegistersAllCoreDirectives(t *testing.T) {
	want := map[string]bool{
		"profile":     true,
		"pipeline":    true,
		"include":     true,
		"action":      true,
		"description": true,
		"require":     true,
	}
	have := map[string]bool{}
	for _, n := range Names() {
		have[n] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("missing registered directive @%s", name)
		}
	}
}

func TestStripDirectivePrefix(t *testing.T) {
	cases := []struct {
		line, name, rest string
		ok               bool
	}{
		{"@profile: trusted", "profile", "trusted", true},
		{"@profile trusted", "profile", "trusted", true},
		{"@profile", "profile", "", true},
		{"@pipeline foo", "pipeline", "foo", true},
		{"@profilefoo", "profile", "", false},
		{"profile: x", "profile", "", false},
	}
	for _, c := range cases {
		rest, ok := StripDirectivePrefix(c.line, c.name)
		if ok != c.ok || rest != c.rest {
			t.Errorf("%q/%q: got (%q,%v), want (%q,%v)",
				c.line, c.name, rest, ok, c.rest, c.ok)
		}
	}
}

func TestSplitNameAndAttrs_HappyPath(t *testing.T) {
	name, attrs, err := SplitNameAndAttrs(`router { provider: "deepseek", model: "deepseek-chat" }`)
	if err != nil {
		t.Fatal(err)
	}
	if name != "router" {
		t.Fatalf("name = %q, want router", name)
	}
	if attrs["provider"] != `"deepseek"` {
		t.Errorf("provider = %q", attrs["provider"])
	}
	if attrs["model"] != `"deepseek-chat"` {
		t.Errorf("model = %q", attrs["model"])
	}
}

func TestSplitNameAndAttrs_NoBraces(t *testing.T) {
	name, attrs, err := SplitNameAndAttrs(`experts`)
	if err != nil {
		t.Fatal(err)
	}
	if name != "experts" || attrs != nil {
		t.Fatalf("got name=%q attrs=%v", name, attrs)
	}
}

func TestSplitNameAndAttrs_RejectsDuplicateAttr(t *testing.T) {
	_, _, err := SplitNameAndAttrs(`x { a: 1, a: 2 }`)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestParseList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`[a, b, c]`, []string{"a", "b", "c"}},
		{`[ "a b", c ]`, []string{"a b", "c"}},
		{`a, b`, []string{"a", "b"}},
		{`[]`, nil},
		{``, nil},
	}
	for _, c := range cases {
		got := ParseList(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: [%d] got %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestTrimQuotes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"abc"`, "abc"},
		{`'abc'`, "abc"},
		{"`abc`", "abc"},
		{`abc`, "abc"},
		{`"abc`, `"abc`},
		{`""`, ""},
	}
	for _, c := range cases {
		if got := TrimQuotes(c.in); got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRegistry_RegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate directive registration")
		}
	}()
	Register(fakeDir{"profile"}) // "profile" already registered by at_profile.go
}

type fakeDir struct{ name string }

func (f fakeDir) Name() string { return f.name }
func (f fakeDir) Apply(*Context, []string, int) (int, error) {
	return 0, nil
}
