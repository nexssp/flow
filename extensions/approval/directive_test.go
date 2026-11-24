package approval

import (
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestDirectiveParsesApprovalBlock(t *testing.T) {
	source := `@approval finance_review {
  role: "finance reviewer"
  reason: "confirm the transfer"
  expires: 45m
}
"ready"`
	clean, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), source, "approval.nflow")
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if clean != "\n\n\n\n\n\"ready\"" {
		t.Fatalf("clean source = %q", clean)
	}
	declarations := DeclarationsFromMeta(meta)
	if len(declarations) != 1 {
		t.Fatalf("declarations = %#v, want one declaration", declarations)
	}
	got := declarations[0]
	if got.Name != "finance_review" || got.Role != "finance reviewer" || got.Reason != "confirm the transfer" || got.Expires.String() != "45m0s" {
		t.Fatalf("declaration = %#v", got)
	}
	if got.Position.File != "approval.nflow" || got.Position.Line != 1 {
		t.Fatalf("position = %#v", got.Position)
	}
}

func TestDirectiveRejectsInvalidAndDuplicateDeclarations(t *testing.T) {
	for name, source := range map[string]string{
		"missing role": `@approval review {
  reason: "check"
}`,
		"unknown field": `@approval review {
  role: reviewer
  reason: check
  owner: team
}`,
		"duplicate": `@approval review {
  role: reviewer
  reason: check
}
@approval review {
  role: reviewer
  reason: check
}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), source, "invalid.nflow")
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestDirectivePreservesClosingBraceInsideQuotedReason(t *testing.T) {
	t.Parallel()
	source := `@approval review {
  role: reviewer
  reason: "verify } transfer"
}`
	_, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), source, "approval-quoted-brace.nflow")
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	declarations := DeclarationsFromMeta(meta)
	if len(declarations) != 1 {
		t.Fatalf("declarations = %#v, want one declaration", declarations)
	}
	if got, want := declarations[0].Reason, "verify } transfer"; got != want {
		t.Fatalf("reason = %q, want %q", got, want)
	}

	apostropheSource := `@approval review {
  role: reviewer
  reason: don't transfer yet
}`
	_, apostropheMeta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), apostropheSource, "approval-apostrophe.nflow")
	if err != nil {
		t.Fatalf("preprocess unquoted apostrophe reason: %v", err)
	}
	apostropheDeclarations := DeclarationsFromMeta(apostropheMeta)
	if len(apostropheDeclarations) != 1 || apostropheDeclarations[0].Reason != "don't transfer yet" {
		t.Fatalf("unquoted apostrophe declaration = %#v", apostropheDeclarations)
	}
}

func TestTokenCovers(t *testing.T) {
	for _, test := range []struct {
		token string
		gate  string
		want  bool
	}{
		{token: "all", gate: "finance", want: true},
		{token: "legal, finance", gate: "finance", want: true},
		{token: "Finance", gate: "finance", want: false},
		{token: "legal", gate: "finance", want: false},
	} {
		if got := TokenCovers(test.token, test.gate); got != test.want {
			t.Errorf("TokenCovers(%q, %q) = %t, want %t", test.token, test.gate, got, test.want)
		}
	}
}
