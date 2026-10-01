package fs

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestIsTestFile(t *testing.T) {
	t.Parallel()
	yes := []string{"a_test.go", "x.test.ts", "y.spec.ts", "z.test.jsx", "test_foo.py"}
	no := []string{"main.go", "index.ts", "helper.py", "test.go", "test_data.json"}
	for _, p := range yes {
		t.Run("yes/"+p, func(t *testing.T) {
			t.Parallel()
			ktest.RequireCondition(t, isTestFile(p), "expected %q to be a test file", p)
		})
	}
	for _, p := range no {
		t.Run("no/"+p, func(t *testing.T) {
			t.Parallel()
			ktest.RequireCondition(t, !isTestFile(p), "expected %q NOT to be a test file", p)
		})
	}
}

func TestIsGeneratedFile(t *testing.T) {
	t.Parallel()
	yes := []string{"api.pb.go", "model_generated.go", "types_generated.ts", "wire.gen.go"}
	no := []string{"main.go", "schema.go", "index.ts", "types.generated.ts"}
	for _, p := range yes {
		t.Run("yes/"+p, func(t *testing.T) {
			t.Parallel()
			ktest.RequireCondition(t, isGeneratedFile(p), "expected %q to be generated", p)
		})
	}
	for _, p := range no {
		t.Run("no/"+p, func(t *testing.T) {
			t.Parallel()
			ktest.RequireCondition(t, !isGeneratedFile(p), "expected %q NOT generated", p)
		})
	}
}

func TestIsDepPath(t *testing.T) {
	t.Parallel()
	yes := []string{"node_modules/lib/index.js", "vendor/x/y.go", "third_party/z"}
	no := []string{"internal/main.go", "src/app.ts"}
	for _, p := range yes {
		ktest.RequireCondition(t, isDepPath(p), "expected %q to be a dep path", p)
	}
	for _, p := range no {
		ktest.RequireCondition(t, !isDepPath(p), "expected %q NOT a dep path", p)
	}
}

func TestIsExamplePath(t *testing.T) {
	t.Parallel()
	yes := []string{"examples/demo.go", "example/x", "samples/y"}
	no := []string{"internal/main.go", "src/app.ts"}
	for _, p := range yes {
		ktest.RequireCondition(t, isExamplePath(p), "expected %q to be an example path", p)
	}
	for _, p := range no {
		ktest.RequireCondition(t, !isExamplePath(p), "expected %q NOT an example path", p)
	}
}

func TestMatchesExclude(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		relPath  string
		patterns []string
		want     bool
	}{
		{"filename glob", "main.go", []string{"*.go"}, true},
		{"full path glob", "internal/main.go", []string{"internal/*.go"}, true},
		{"directory prefix", "testdata/x.go", []string{"testdata/"}, true},
		{"directory mid path", "src/testdata/x.go", []string{"testdata/"}, true},
		{"no match", "main.go", []string{"*.md"}, false},
		{"empty patterns", "main.go", nil, false},
		{"empty entry skipped", "main.go", []string{"  "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := matchesExclude(c.relPath, c.patterns)
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestFilter_CombinedRules(t *testing.T) {
	t.Parallel()
	in := []FileMeta{
		{Path: "/root/node_modules/a_test.go", RelPath: "node_modules/a_test.go", Lang: "go"},
		{Path: "/root/main.go", RelPath: "main.go", Lang: "go"},
	}

	got := collectFilter(t, FilterConfig{Tests: true, Deps: true}, in)
	ktest.RequireEqual(t, len(got), 1)
	ktest.RequireEqual(t, got[0].RelPath, "main.go")

	all := collectFilter(t, FilterConfig{}, in)
	ktest.RequireEqual(t, len(all), 2)
}

// collectFilter runs the filter operator over an in-memory slice and
// returns the surviving items.
func collectFilter(tb testing.TB, cfg FilterConfig, in []FileMeta) []FileMeta {
	tb.Helper()
	var out []FileMeta
	for meta, err := range Filter(cfg)(sliceSeq(in)) {
		ktest.RequireNoError(tb, err)
		out = append(out, meta)
	}
	return out
}

func TestSplitList(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"":        nil,
		"a":       {"a"},
		"a,b,c":   {"a", "b", "c"},
		" a , b ": {"a", "b"}, //nolint:gocritic // intentional whitespace for key-normalization test
		"a,,b":    {"a", "b"},
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, splitList(in), want)
		})
	}
}
