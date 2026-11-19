package pipeline

import (
	"strings"
	"testing"

	compiler "github.com/nexssp/flow/compiler"
)

func TestParsePipelineDeclarationHeaderSignatures(t *testing.T) {
	tests := []struct {
		name          string
		header        string
		wantSignature bool
		wantInputName string
		wantInput     string
		wantOutput    string
		wantModifiers []string
	}{
		{
			name:          "typed input and output with a legacy modifier",
			header:        `@pipeline create_order(order_req: !CreateRequest): !CreateResponse :timeout=5s`,
			wantSignature: true,
			wantInputName: "order_req",
			wantInput:     "!CreateRequest",
			wantOutput:    "!CreateResponse",
			wantModifiers: []string{"timeout=5s"},
		},
		{
			name:          "input only",
			header:        `@pipeline lookup(req: LookupRequest)`,
			wantSignature: true,
			wantInputName: "req",
			wantInput:     "LookupRequest",
		},
		{
			name:          "output only",
			header:        `@pipeline response(): !Response`,
			wantSignature: true,
			wantOutput:    "!Response",
		},
		{
			name:          "empty signature",
			header:        `@pipeline empty()`,
			wantSignature: true,
		},
		{
			name:          "existing untyped form",
			header:        `@pipeline legacy :retry=2`,
			wantModifiers: []string{"retry=2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{tc.header, "  noop", "@end"}
			header, signature, hasSignature, err := parsePipelineDeclarationHeader(compiler.DirectiveReq{
				Lines: lines, File: "signature.nflow",
			})
			if err != nil {
				t.Fatalf("parsePipelineDeclarationHeader() error = %v", err)
			}
			if hasSignature != tc.wantSignature {
				t.Fatalf("hasSignature = %t, want %t", hasSignature, tc.wantSignature)
			}
			if header.Name == "" {
				t.Fatal("parsed pipeline name is empty")
			}
			if len(header.Modifiers) != len(tc.wantModifiers) {
				t.Fatalf("modifiers = %#v, want %#v", header.Modifiers, tc.wantModifiers)
			}
			for i := range tc.wantModifiers {
				if header.Modifiers[i] != tc.wantModifiers[i] {
					t.Fatalf("modifiers = %#v, want %#v", header.Modifiers, tc.wantModifiers)
				}
			}
			if signature.inputName != tc.wantInputName {
				t.Fatalf("input name = %q, want %q", signature.inputName, tc.wantInputName)
			}
			if got := selectionValue(signature.input); got != tc.wantInput {
				t.Fatalf("input selection = %q, want %q", got, tc.wantInput)
			}
			if got := selectionValue(signature.output); got != tc.wantOutput {
				t.Fatalf("output selection = %q, want %q", got, tc.wantOutput)
			}
			if hasSignature && header.HeaderSpan.End != len(tc.header) {
				t.Fatalf("header span end = %d, want original header length %d", header.HeaderSpan.End, len(tc.header))
			}
		})
	}
}

func TestParsePipelineDeclarationHeaderRejectsInvalidSignatures(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   string
	}{
		{header: `@pipeline lookup(input: Request)`, want: "reserved"},
		{header: `@pipeline lookup(bad-name: Request)`, want: "invalid input parameter name"},
		{header: `@pipeline lookup(req: Request, req: Other)`, want: "one named parameter"},
		{header: `@pipeline lookup(req: bad-name)`, want: "invalid input type"},
		{header: `@pipeline response(): !`, want: "invalid output type"},
		{header: `@pipeline lookup(req: Request`, want: "unterminated pipeline signature"},
	} {
		t.Run(tc.header, func(t *testing.T) {
			_, _, _, err := parsePipelineDeclarationHeader(compiler.DirectiveReq{
				Lines: []string{tc.header, "@end"}, File: "invalid-signature.nflow",
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parse error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func selectionValue(selection *schemaSelection) string {
	if selection == nil {
		return ""
	}
	return selection.value()
}
