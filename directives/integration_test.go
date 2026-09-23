package directives_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nexssp/flow/directives"
)

// TestDomainlessFlowIntegration exercises every domainless directive
// through the same Lookup/Apply loop that flow.Preprocess uses. It
// verifies that the directive layer, taken alone, produces a fully
// populated Preprocessed for a .nflow that uses no domain extensions.
//
// The test deliberately runs the loop by hand rather than calling
// flow.PreprocessBytes, because flow/directives cannot import flow
// (flow imports flow/directives; a test-only import of the parent
// would create a cycle).
func TestDomainlessFlowIntegration(t *testing.T) {
	src := []string{
		`# comment line`,
		`@description "Code review pipeline"`,
		`@profile: untrusted_input`,
		`@requires: llm[deepseek], sandbox[golang:1.26], network[api.github.com]`,
		`@config:budget_usd=0.50`,
		`@config:approval=danger`,
		`@assert: success == true`,
		`@assert: cost_usd < 0.50`,
		``,
		`@pipeline scan`,
		`  sandbox.sec @{ code: .source_code }`,
		`  -> { passed: .exit_code == 0 }`,
		`@end`,
		``,
		`spec.parser -> scan`,
	}

	pre := runPreprocess(t, src)

	if pre.Description != "Code review pipeline" {
		t.Errorf("description = %q", pre.Description)
	}
	if pre.Profile != "untrusted_input" {
		t.Errorf("profile = %q", pre.Profile)
	}
	if len(pre.Asserts) != 2 {
		t.Fatalf("expected 2 asserts, got %d", len(pre.Asserts))
	}
	if pre.Asserts[0].Expr != "success == true" {
		t.Errorf("assert[0] = %q", pre.Asserts[0].Expr)
	}
	if pre.Asserts[0].Pos.Line != 7 {
		t.Errorf("assert[0] line = %d, want 7", pre.Asserts[0].Pos.Line)
	}
	if pre.Config["budget_usd"] != "0.50" {
		t.Errorf("config budget_usd = %q", pre.Config["budget_usd"])
	}
	if pre.Config["approval"] != "danger" {
		t.Errorf("config approval = %q", pre.Config["approval"])
	}

	caps := pre.Capabilities
	if len(caps.LLM) != 1 || caps.LLM[0] != "deepseek" {
		t.Errorf("LLM = %v", caps.LLM)
	}
	if len(caps.Sandbox) != 1 || caps.Sandbox[0] != "golang:1.26" {
		t.Errorf("Sandbox = %v", caps.Sandbox)
	}
	if len(caps.Network) != 1 || caps.Network[0] != "api.github.com" {
		t.Errorf("Network = %v", caps.Network)
	}

	if len(pre.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(pre.Pipelines))
	}
	if pre.Pipelines[0].Name != "scan" {
		t.Errorf("pipeline name = %q", pre.Pipelines[0].Name)
	}
	if !strings.Contains(pre.Pipelines[0].Body, "sandbox.sec") {
		t.Errorf("pipeline body missing sandbox.sec: %q", pre.Pipelines[0].Body)
	}

	if !strings.Contains(pre.DSL, "spec.parser -> scan") {
		t.Errorf("DSL missing executable pipeline: %q", pre.DSL)
	}

	// Line alignment: the executable line must land on the same index
	// it has in the source (15 lines before it, index 14).
	lines := strings.Split(pre.DSL, "\n")
	if len(lines) != len(src) {
		t.Fatalf("DSL line count = %d, want %d (line alignment broken)",
			len(lines), len(src))
	}
	if strings.TrimSpace(lines[14]) != "spec.parser -> scan" {
		t.Errorf("DSL[14] = %q, want executable line", lines[14])
	}
}

// TestDomainlessFlow_RejectsUnknownProfile verifies that a typo in
// @profile: is caught at preprocess time, not silently accepted.
func TestDomainlessFlow_RejectsUnknownProfile(t *testing.T) {
	src := []string{`@profile: untrusted_inpt`}

	_, err := runPreprocessE(src)
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
	if !strings.Contains(err.Error(), "unknown profile") {
		t.Errorf("error = %v", err)
	}
}

// TestDomainlessFlow_RejectsUnknownCapabilityDomain verifies that
// @requires: rejects a domain name that no runtime knows about.
func TestDomainlessFlow_RejectsUnknownCapabilityDomain(t *testing.T) {
	src := []string{`@requires: quantum[foo]`}

	_, err := runPreprocessE(src)
	if err == nil {
		t.Fatal("expected error for unknown capability domain")
	}
	if !strings.Contains(err.Error(), "unknown capability domain") {
		t.Errorf("error = %v", err)
	}
}

// ─── test harness ──────────────────────────────────────────────────────

func runPreprocess(t *testing.T, lines []string) *directives.Preprocessed {
	t.Helper()
	pre, err := runPreprocessE(lines)
	if err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	return pre
}

func runPreprocessE(lines []string) (*directives.Preprocessed, error) {
	out := &directives.Preprocessed{
		File:         "test.nflow",
		Config:       map[string]string{},
		Declarations: map[string]any{},
	}
	ctx := &directives.Context{
		Out:  out,
		File: "test.nflow",
		Body: make([]string, len(lines)),
		ValidateProfile: func(name string) error {
			switch name {
			case "trusted", "untrusted_input", "network_isolated":
				return nil
			default:
				return errors.New("unknown profile " + name)
			}
		},
	}

	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if d, ok := directives.Lookup(trimmed); ok {
			next, err := d.Apply(ctx, lines, i)
			if err != nil {
				return nil, err
			}
			i = next
			continue
		}
		ctx.Body[i] = lines[i]
		i++
	}
	out.DSL = strings.Join(ctx.Body, "\n")
	return out, nil
}
