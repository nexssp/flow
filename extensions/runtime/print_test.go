package runtime

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func renderToString(v any, cfg printConfig) string {
	return renderPrint(v, cfg, false)
}

func defaultConfig() printConfig {
	return printConfig{
		limit:   printDefaultLimit,
		depth:   printDefaultDepth,
		strings: printDefaultStrings,
		stream:  "stderr",
	}
}

func TestPrint_Scalar(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		in   any
		want string
	}{
		{nil, "null"},
		{true, "true"},
		{false, "false"},
		{42, "42"},
		{3.14, "3.14"},
		{`hello`, `"hello"`},
	} {
		t.Run(c.want, func(t *testing.T) {
			t.Parallel()
			got := renderToString(c.in, defaultConfig())
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestPrint_Map(t *testing.T) {
	t.Parallel()

	got := renderToString(map[string]any{"b": 2, "a": 1}, defaultConfig())
	// keys sorted, multi-line
	ktest.RequireStringContains(t, got, `"a": 1`)
	ktest.RequireStringContains(t, got, `"b": 2`)
	ktest.RequireCondition(t, strings.Index(got, `"a"`) < strings.Index(got, `"b"`), "keys must be sorted")
}

func TestPrint_SliceInline(t *testing.T) {
	t.Parallel()

	got := renderToString([]any{"a", "b", "c"}, defaultConfig())
	ktest.RequireEqual(t, got, `["a", "b", "c"]`)
}

func TestPrint_SliceMultiline(t *testing.T) {
	t.Parallel()

	got := renderToString([]any{
		map[string]any{"x": 1},
		map[string]any{"y": 2},
	}, defaultConfig())
	ktest.RequireStringContains(t, got, "[\n")
	ktest.RequireStringContains(t, got, `"x": 1`)
}

func TestPrint_Limit(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig()
	cfg.limit = 2
	in := []any{1, 2, 3, 4, 5}
	got := renderToString(in, cfg)
	ktest.RequireStringContains(t, got, "... 3 more")
	ktest.RequireStringContains(t, got, "1")
	ktest.RequireStringContains(t, got, "2")
	ktest.RequireStringNotContains(t, got, ", 3")
}

func TestPrint_Depth(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig()
	cfg.depth = 1
	in := map[string]any{
		"a": map[string]any{
			"b": map[string]any{"c": 1},
		},
	}
	got := renderToString(in, cfg)
	ktest.RequireStringContains(t, got, "...")
	ktest.RequireStringNotContains(t, got, `"c"`)
}

func TestPrint_StringsTruncate(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig()
	cfg.strings = 5
	got := renderToString("hello world this is long", cfg)
	ktest.RequireStringContains(t, got, `"hello...`)
	ktest.RequireStringNotContains(t, got, "long")
}

func TestPrint_ByteBudget(t *testing.T) {
	t.Parallel()

	big := make([]any, 1000)
	for i := range big {
		big[i] = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	}
	cfg := printConfig{limit: 1000, depth: 4, strings: 200, stream: "stderr"}
	got := renderPrint(big, cfg, false)
	ktest.RequireCondition(t, len(got) <= printMaxBytes+64, "output %d exceeds cap", len(got))
	ktest.RequireStringContains(t, got, "truncated")
}

func TestPrint_StripConfigKeys(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"x":      1,
		"label":  "step1",
		"limit":  10,
		"depth":  3,
		"stream": "stderr",
	}
	got := stripPrintConfig(in)
	m, ok := got.(map[string]any)
	ktest.RequireCondition(t, ok, "expected map, got %T", got)
	ktest.RequireEqual(t, len(m), 1)
	ktest.RequireEqual(t, m["x"], 1)
}

func TestPrint_ExtractConfig(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"label":   "step1",
		"limit":   float64(10),
		"depth":   float64(2),
		"strings": float64(50),
		"stream":  "stdout",
	}
	cfg := extractPrintConfig(in)
	ktest.RequireEqual(t, cfg.label, "step1")
	ktest.RequireEqual(t, cfg.limit, 10)
	ktest.RequireEqual(t, cfg.depth, 2)
	ktest.RequireEqual(t, cfg.strings, 50)
	ktest.RequireEqual(t, cfg.stream, "stdout")
}

func TestPrint_ExtractConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := extractPrintConfig(map[string]any{})
	ktest.RequireEqual(t, cfg.limit, printDefaultLimit)
	ktest.RequireEqual(t, cfg.depth, printDefaultDepth)
	ktest.RequireEqual(t, cfg.strings, printDefaultStrings)
	ktest.RequireEqual(t, cfg.stream, "stderr")
}

func TestPrint_ColorsDisabledWhenNotTTY(t *testing.T) {
	t.Parallel()

	got := renderPrint(map[string]any{"k": 1}, defaultConfig(), false)
	ktest.RequireStringNotContains(t, got, "\x1b[")
}

func TestPrint_ColorsEnabledWhenTTY(t *testing.T) {
	t.Parallel()

	got := renderPrint(map[string]any{"k": 1}, defaultConfig(), true)
	ktest.RequireStringContains(t, got, "\x1b[")
}

func TestPrint_WriteOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writePrintOutput(&buf, "step1", "{}")
	ktest.RequireEqual(t, buf.String(), "[step1] {}\n")

	buf.Reset()
	writePrintOutput(&buf, "", "{}")
	ktest.RequireEqual(t, buf.String(), "{}\n")
}
