package symbols

import (
	"context"
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

//go:embed nflows
var fixturesFS embed.FS

const ID = "symbols"

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []compiler.Directive{Directive},
		Fixtures:   fixturesFS,
		WrapPipeline: func(meta map[string]any, inner action.AnyAction) (action.AnyAction, error) {
			syms, ok := meta["symbols"].(map[string]compiler.SymbolNamespace)
			if !ok || len(syms) == 0 {
				return inner, nil
			}
			return action.New("symbols.wrap", func(ctx context.Context, req any) (any, error) {
				return action.InvokeAny(compiler.WithLocalSymbols(ctx, syms), inner, req)
			}).Build(), nil
		},
	}
}
