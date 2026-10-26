package lang

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestPythonBlockKeepsBracesInsideCommentsAndStrings(t *testing.T) {
	for key := range Registry {
		delete(Registry, key)
	}
	aliases = nil
	if err := LoadRunners(); err != nil {
		t.Fatalf("LoadRunners: %v", err)
	}

	source := `py {
#comment brace must not close the block: }
record = {"literal": "}", "triple": """a brace } inside text"""}
message = f"value is {record['literal']}"
print(message)
}`
	parser := compiler.NewParserWithFile(context.Background(), compiler.NewOperatorTable(),
		compiler.NewPrimaryExtensionTable(keywordPrimaries()...), source, "analysis.nflow")
	expr, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse Python block: %v", err)
	}
	blockExpr, ok := expr.(*langBlockExpr)
	if !ok {
		t.Fatalf("Parse returned %T; want *langBlockExpr", expr)
	}
	if !strings.Contains(blockExpr.block.Source, "#comment brace must not close the block: }") {
		t.Errorf("Python comment was truncated from block source: %q", blockExpr.block.Source)
	}
	if !strings.Contains(blockExpr.block.Source, `"""a brace } inside text"""`) {
		t.Errorf("triple-quoted string was truncated from block source: %q", blockExpr.block.Source)
	}
	if !strings.Contains(blockExpr.block.Source, `f"value is {record['literal']}"`) {
		t.Errorf("f-string source was truncated from block source: %q", blockExpr.block.Source)
	}
}
