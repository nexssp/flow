package require

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateGlueIsDeterministic(t *testing.T) {
	t.Parallel()

	modules := []ResolvedModule{
		{Requirement: Requirement{Import: "example.com/z", Options: map[string]string{}}},
		{Requirement: Requirement{
			Import:  "example.com/a",
			Options: map[string]string{"bundle": "CustomBundle"},
		}},
	}

	var first, second bytes.Buffer
	if err := GenerateGlue(&first, "main", "", modules); err != nil {
		t.Fatalf("first generation failed: %v", err)
	}
	if err := GenerateGlue(&second, "main", "", modules); err != nil {
		t.Fatalf("second generation failed: %v", err)
	}

	if first.String() != second.String() {
		t.Fatal("GenerateGlue output is non-deterministic")
	}

	output := first.String()
	expected := []string{
		"package main",
		`nexsscli "github.com/nexssp/flow/cli"`,
		`nexssreq0 "example.com/a"`,
		`nexssreq1 "example.com/z"`,
		"func init() {",
		"nexsscli.RegisterBundle(",
		"nexssreq0.CustomBundle(",
		"nexssreq1.Bundle(nil)",
	}

	for _, want := range expected {
		if !strings.Contains(output, want) {
			t.Fatalf("generated glue missing %q:\n%s", want, output)
		}
	}
}

func TestGenerateGlueRejectsInvalidIdentifiers(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	if err := GenerateGlue(&buf, "bad-package-name", "", nil); err == nil {
		t.Fatal("expected error on invalid package identifier")
	}

	malicious := []ResolvedModule{{
		Requirement: Requirement{
			Import:  "example.com/a",
			Options: map[string]string{"bundle": "malicious(); //"},
		},
	}}
	if err := GenerateGlue(&buf, "main", "", malicious); err == nil {
		t.Fatal("expected error on invalid bundle function identifier")
	}
}
