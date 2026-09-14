// nexssp/flow/compiler/example_test.go
package compiler_test

import (
	"fmt"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestParser_Example(t *testing.T) {
	input := `github.issue:arch#pkg~testdata@security -> { prompt: "Triage: " + issue.title } -> ai.triage`
	parser := compiler.NewParser(input)

	ast, err := parser.ParseExpression()
	if err != nil {
		t.Fatal(err)
	}

	// Print AST (simplified)
	fmt.Printf("AST: %T\n", ast)
	// Walk the tree (we'll just show structure)
	if pipe, ok := ast.(*compiler.PipelineExpr); ok {
		fmt.Println("Pipeline: left =", pipe.Left, "right =", pipe.Right)
	}
	// Output would show the parsed structure.
}
