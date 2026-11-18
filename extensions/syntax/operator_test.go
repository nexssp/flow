package syntax

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestSequentialPipelineUsesArrowOnly(t *testing.T) {
	t.Parallel()

	operators := compiler.NewOperatorTable(Bundle(nil).Operators...)
	tests := []struct {
		name      string
		source    string
		wantError string
		wantType  any
	}{
		{name: "arrow", source: "left -> right", wantType: (*compiler.PipeExpr)(nil)},
		{name: "legacy single pipe", source: "left | right", wantError: `unexpected token "|"`},
		{name: "parallel ampersand", source: "(left & right)", wantType: (*compiler.ParallelExpr)(nil)},
		{name: "fallback double pipe", source: "left || right", wantType: (*compiler.FallbackExpr)(nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parsed, err := compiler.NewParserWithFile(
				context.Background(), operators, nil, test.source, "operators.nflow",
			).Parse()
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("parse error = %v, want it to contain %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse %q: %v", test.source, err)
			}
			switch test.wantType.(type) {
			case *compiler.PipeExpr:
				if _, ok := parsed.(*compiler.PipeExpr); !ok {
					t.Fatalf("parsed %q as %T, want *compiler.PipeExpr", test.source, parsed)
				}
			case *compiler.ParallelExpr:
				if _, ok := parsed.(*compiler.ParallelExpr); !ok {
					t.Fatalf("parsed %q as %T, want *compiler.ParallelExpr", test.source, parsed)
				}
			case *compiler.FallbackExpr:
				if _, ok := parsed.(*compiler.FallbackExpr); !ok {
					t.Fatalf("parsed %q as %T, want *compiler.FallbackExpr", test.source, parsed)
				}
			}
		})
	}
}
