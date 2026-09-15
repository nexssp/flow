package capability

import "testing"

func TestParseBindings(t *testing.T) {
	manifest := `
# comment
@config:budget=1000

agent.orchestrator:remote="http://10.0.0.5:9001/solve":timeout=30s@Decompose
  -> agent.worker:remote="http://10.0.0.6:9002/work":timeout=45s@Implement
  -> sandbox.test_runner:timeout=30s
  -> tools.ripgrep:exec="rg --json":timeout=10s@Search

skills.lint:wasm="./skills/lint.wasm":timeout=15s@Lint
`
	got := ParseBindings(manifest)

	want := []struct {
		name   string
		kind   Kind
		target string
	}{
		{"agent.orchestrator", KindHTTP, "http://10.0.0.5:9001/solve"},
		{"agent.worker", KindHTTP, "http://10.0.0.6:9002/work"},
		{"tools.ripgrep", KindExec, "rg --json"},
		{"skills.lint", KindWASM, "./skills/lint.wasm"},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d bindings, want %d: %+v", len(got), len(want), got)
	}

	for i, w := range want {
		g := got[i]
		if g.Name != w.name || g.Kind != w.kind || g.Target != w.target {
			t.Errorf("binding %d: got %+v, want name=%q kind=%v target=%q",
				i, g, w.name, w.kind, w.target)
		}
	}
}

func TestParseBindings_IgnoresNonHTTPRemote(t *testing.T) {
	// A :remote= that is not a URL is silently ignored — user likely meant
	// :exec= and typed the wrong key.
	got := ParseBindings(`x:remote="not-a-url"`)
	if len(got) != 0 {
		t.Errorf("expected no bindings, got %+v", got)
	}
}

func TestTailAtom_IgnoresArrowsInQuotes(t *testing.T) {
	line := `a:payload="x -> y" -> b:role=admin`
	if got := tailAtom(line); got != "b:role=admin" {
		t.Errorf("got %q", got)
	}
}

func TestSplitMods_RespectsQuotes(t *testing.T) {
	mods := splitMods(`a=1:b="x:y":c=3`)

	want := []string{`a=1`, `b="x:y"`, `c=3`}
	if len(mods) != len(want) {
		t.Fatalf("got %v, want %v", mods, want)
	}

	for i := range want {
		if mods[i] != want[i] {
			t.Errorf("mod %d: got %q, want %q", i, mods[i], want[i])
		}
	}
}
