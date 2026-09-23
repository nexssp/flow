package dslparse_test

import (
	"testing"

	"github.com/nexssp/flow/dslparse"
)

func TestParseSchemaAndTypedModifiers(t *testing.T) {
	line, ok := dslparse.ParseLine(`ai.prompt:model="deepseek-flash":schema="Verdict"`)
	if !ok {
		t.Fatal("ParseLine rejected schema modifier")
	}
	if line.Modifiers.Model != "deepseek-flash" {
		t.Fatalf("model=%q", line.Modifiers.Model)
	}
	if line.Modifiers.Schema != "Verdict" {
		t.Fatalf("schema=%q", line.Modifiers.Schema)
	}

	line, ok = dslparse.ParseLine(`ai.prompt:typed=Verdict`)
	if !ok {
		t.Fatal("ParseLine rejected typed modifier")
	}
	if line.Modifiers.Typed != "Verdict" {
		t.Fatalf("typed=%q", line.Modifiers.Typed)
	}
}
