package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestExplainASTIncludesAssertionMessageExpression(t *testing.T) {
	t.Parallel()
	assertion := &compiler.AssertExpr{
		Condition:         ".valid",
		MessageExpression: `"expected " + .expected`,
	}

	node := astNodeOf(assertion)
	if node.MessageExpression != assertion.MessageExpression {
		t.Fatalf("AST message expression = %q, want %q", node.MessageExpression, assertion.MessageExpression)
	}

	var text bytes.Buffer
	renderASTNode(&text, assertion, "", false)
	if !strings.Contains(text.String(), "message expression: "+assertion.MessageExpression) {
		t.Fatalf("text AST output %q omits the message expression", text.String())
	}

	var jsonOutput bytes.Buffer
	if err := writeExplainASTJSON(&jsonOutput, "assert.nflow", assertion, nil); err != nil {
		t.Fatalf("write JSON AST: %v", err)
	}
	var report struct {
		TopLevel struct {
			MessageExpression string `json:"message_expression"`
		} `json:"top_level"`
	}
	if err := json.Unmarshal(jsonOutput.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON AST: %v", err)
	}
	if report.TopLevel.MessageExpression != assertion.MessageExpression {
		t.Fatalf("JSON AST message expression = %q, want %q", report.TopLevel.MessageExpression, assertion.MessageExpression)
	}
}
