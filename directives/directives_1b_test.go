package directives

import (
	"strings"
	"testing"
)

// ─── @requires ─────────────────────────────────────────────────────────

func TestAtRequires_HappyPath(t *testing.T) {
	ctx := newTestCtx()
	d := atRequires{}

	line := `@requires: llm[openai,deepseek], sandbox[golang:1.26], network[api.github.com]`
	next, err := d.Apply(ctx, []string{line}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != 1 {
		t.Fatalf("next = %d, want 1", next)
	}

	c := ctx.Out.Capabilities
	if len(c.LLM) != 2 || c.LLM[0] != "openai" || c.LLM[1] != "deepseek" {
		t.Errorf("LLM = %v", c.LLM)
	}
	if len(c.Sandbox) != 1 || c.Sandbox[0] != "golang:1.26" {
		t.Errorf("Sandbox = %v", c.Sandbox)
	}
	if len(c.Network) != 1 || c.Network[0] != "api.github.com" {
		t.Errorf("Network = %v", c.Network)
	}
}

func TestAtRequires_MergesMultipleLines(t *testing.T) {
	ctx := newTestCtx()
	d := atRequires{}

	_, err := d.Apply(ctx, []string{`@requires: llm[openai]`}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Apply(ctx, []string{`@requires: llm[deepseek], network[api.x.com]`}, 0)
	if err != nil {
		t.Fatal(err)
	}

	c := ctx.Out.Capabilities
	if len(c.LLM) != 2 {
		t.Fatalf("expected 2 LLMs after merge, got %v", c.LLM)
	}
	if len(c.Network) != 1 {
		t.Fatalf("expected 1 network after merge, got %v", c.Network)
	}
}

func TestAtRequires_RejectsUnknownDomain(t *testing.T) {
	ctx := newTestCtx()
	_, err := atRequires{}.Apply(ctx, []string{`@requires: quantum[foo]`}, 0)
	if err == nil || !strings.Contains(err.Error(), "unknown capability domain") {
		t.Fatalf("expected unknown-domain error, got %v", err)
	}
}

func TestAtRequires_RejectsMissingBracket(t *testing.T) {
	ctx := newTestCtx()
	_, err := atRequires{}.Apply(ctx, []string{`@requires: llm`}, 0)
	if err == nil || !strings.Contains(err.Error(), "expected `name[...]`") {
		t.Fatalf("expected bracket error, got %v", err)
	}
}

func TestAtRequires_RejectsEmpty(t *testing.T) {
	ctx := newTestCtx()
	_, err := atRequires{}.Apply(ctx, []string{`@requires:`}, 0)
	if err == nil {
		t.Fatal("expected error on empty @requires")
	}
}

// ─── @config ───────────────────────────────────────────────────────────

func TestAtConfig_HappyPath(t *testing.T) {
	ctx := newTestCtx()
	lines := []string{`@config:budget_usd=0.50`}

	next, err := atConfig{}.Apply(ctx, lines, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != 1 {
		t.Fatalf("next = %d, want 1", next)
	}
	if ctx.Out.Config["budget_usd"] != "0.50" {
		t.Errorf("budget_usd = %q", ctx.Out.Config["budget_usd"])
	}
	// Body must preserve the original line for the current reader.
	if ctx.Body[0] != lines[0] {
		t.Errorf("Body[0] = %q, want original", ctx.Body[0])
	}
}

func TestAtConfig_StripsQuotes(t *testing.T) {
	ctx := newTestCtx()
	_, err := atConfig{}.Apply(ctx, []string{`@config:approval="danger"`}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Out.Config["approval"] != "danger" {
		t.Errorf("approval = %q, want danger", ctx.Out.Config["approval"])
	}
}

func TestAtConfig_LaterOverridesEarlier(t *testing.T) {
	ctx := newTestCtx()
	d := atConfig{}
	_, _ = d.Apply(ctx, []string{`@config:max_tokens=1024`}, 0)
	_, _ = d.Apply(ctx, []string{`@config:max_tokens=4096`}, 0)
	if ctx.Out.Config["max_tokens"] != "4096" {
		t.Errorf("max_tokens = %q, want 4096", ctx.Out.Config["max_tokens"])
	}
}

func TestAtConfig_RejectsMissingEquals(t *testing.T) {
	ctx := newTestCtx()
	_, err := atConfig{}.Apply(ctx, []string{`@config:budget_usd`}, 0)
	if err == nil || !strings.Contains(err.Error(), "expected key=value") {
		t.Fatalf("expected key=value error, got %v", err)
	}
}

func TestAtConfig_RejectsEmptyKey(t *testing.T) {
	ctx := newTestCtx()
	_, err := atConfig{}.Apply(ctx, []string{`@config:=0.50`}, 0)
	if err == nil || !strings.Contains(err.Error(), "empty key") {
		t.Fatalf("expected empty-key error, got %v", err)
	}
}

// ─── @assert ───────────────────────────────────────────────────────────

func TestAtAssert_CollectsExpressions(t *testing.T) {
	ctx := newTestCtx()
	d := atAssert{}

	_, err := d.Apply(ctx, []string{`@assert: success == true`}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Apply(ctx, []string{`@assert: cost_usd < 0.50`}, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(ctx.Out.Asserts) != 2 {
		t.Fatalf("expected 2 asserts, got %d", len(ctx.Out.Asserts))
	}
	if ctx.Out.Asserts[0].Expr != "success == true" {
		t.Errorf("assert[0] = %q", ctx.Out.Asserts[0].Expr)
	}
	if ctx.Out.Asserts[1].Expr != "cost_usd < 0.50" {
		t.Errorf("assert[1] = %q", ctx.Out.Asserts[1].Expr)
	}
	if ctx.Out.Asserts[0].Pos.Line != 1 {
		t.Errorf("assert[0].Pos.Line = %d, want 1", ctx.Out.Asserts[0].Pos.Line)
	}
}

func TestAtAssert_RejectsEmpty(t *testing.T) {
	ctx := newTestCtx()
	_, err := atAssert{}.Apply(ctx, []string{`@assert:`}, 0)
	if err == nil {
		t.Fatal("expected error on empty @assert")
	}
}

func TestAtAssert_DirectiveLineIsBlankedInBody(t *testing.T) {
	ctx := newTestCtx()
	line := `@assert: x == 1`
	_, _ = atAssert{}.Apply(ctx, []string{line}, 0)
	if strings.TrimSpace(ctx.Body[0]) != "" {
		t.Errorf("Body[0] = %q, want blank", ctx.Body[0])
	}
}

// ─── helpers ───────────────────────────────────────────────────────────

func newTestCtx() *Context {
	return &Context{
		Out: &Preprocessed{
			File:         "test.nflow",
			Config:       map[string]string{},
			Declarations: map[string]any{},
		},
		Body: make([]string, 32),
	}
}
