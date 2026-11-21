package loop

import (
	"context"
	"reflect"
	"strconv"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/nodes_distribute"
	"github.com/nexssp/flow/internal/lexical"
)

const (
	eachKeywordName       = "each"
	eachConcurrencyOption = "concurrency"
)

type eachKeyword struct{}

func (eachKeyword) Name() string    { return eachKeywordName }
func (eachKeyword) Keyword() string { return eachKeywordName }

func (eachKeyword) Parse(p *compiler.Parser) (compiler.Expr, error) {
	position := p.Position()
	p.Advance() // consume `each`

	concurrency := 0
	if p.Current().Type == compiler.TokColon {
		var err error
		concurrency, err = parseEachConcurrency(p)
		if err != nil {
			return nil, err
		}
		if p.Current().Type == compiler.TokColon {
			if p.Peek(1).Type == compiler.TokIdent && p.Peek(1).Lit == eachConcurrencyOption {
				return nil, p.Fail(p.Current().Line, "each: duplicate :concurrency option")
			}
			return nil, p.Fail(p.Current().Line,
				"each: only :concurrency may appear before the block; put other modifiers after the block or in @scope")
		}
	}

	body, err := parseEachBlock(p)
	if err != nil {
		return nil, err
	}

	if p.Current().Type == compiler.TokColon && p.Peek(1).Type == compiler.TokIdent && p.Peek(1).Lit == eachConcurrencyOption {
		if concurrency != 0 {
			return nil, p.Fail(p.Current().Line, "each: duplicate :concurrency option")
		}
		concurrency, err = parseEachConcurrency(p)
		if err != nil {
			return nil, err
		}
		if p.Current().Type == compiler.TokColon && p.Peek(1).Type == compiler.TokIdent && p.Peek(1).Lit == eachConcurrencyOption {
			return nil, p.Fail(p.Current().Line, "each: duplicate :concurrency option")
		}
	}

	return &EachExpr{Body: body, Concurrency: concurrency, Pos: position}, nil
}

func parseEachConcurrency(p *compiler.Parser) (int, error) {
	if p.Current().Type != compiler.TokColon || p.Peek(1).Type != compiler.TokIdent || p.Peek(1).Lit != eachConcurrencyOption {
		return 0, p.Fail(p.Current().Line, "each: expected :concurrency=NUMBER")
	}
	p.Advance() // consume ':'
	p.Advance() // consume 'concurrency'
	if p.Current().Type != compiler.TokEquals {
		return 0, p.Fail(p.Current().Line, "each: expected '=' after :concurrency")
	}
	p.Advance()
	if p.Current().Type != compiler.TokNumber {
		return 0, p.Fail(p.Current().Line, "each: concurrency must be an integer")
	}
	normalized, err := lexical.NormalizeNumericSeparators(p.Current().Lit)
	if err != nil {
		return 0, p.Fail(p.Current().Line, "each: concurrency must be an integer")
	}
	value, err := strconv.Atoi(normalized)
	if err != nil {
		return 0, p.Fail(p.Current().Line, "each: concurrency must be an integer")
	}
	if value <= 0 {
		return 0, p.Fail(p.Current().Line, "each: concurrency must be positive")
	}
	p.Advance()
	return value, nil
}

func parseEachBlock(p *compiler.Parser) (compiler.Expr, error) {
	if p.Current().Type != compiler.TokLBrace {
		return nil, p.Fail(p.Current().Line, "each: expected '{' before block")
	}
	p.Advance()
	if p.Current().Type == compiler.TokRBrace {
		return nil, p.Fail(p.Current().Line, "each: block must contain a Flow expression")
	}

	body, err := p.ParseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.Current().Type != compiler.TokRBrace {
		if p.Current().Type == compiler.TokInvalid {
			return nil, p.Fail(p.Current().Line, "unexpected character %q", p.Current().Lit)
		}
		return nil, p.Fail(p.Current().Line, "each: expected '}' closing block")
	}
	p.Advance()
	return body, nil
}

// EachExpr maps its body over the current list, with isolated expression
// bindings for each item and the surrounding input scope inherited read-only.
type EachExpr struct {
	Body        compiler.Expr
	Concurrency int
	Pos         compiler.Position
}

func (*EachExpr) Node() {}

func (e *EachExpr) Position() compiler.Position { return e.Pos }

func (e *EachExpr) Children() []compiler.Expr {
	if e.Body == nil {
		return nil
	}
	return []compiler.Expr{e.Body}
}

func (e *EachExpr) DescribeActionGraph() compiler.ActionGraphDescription {
	return compiler.ActionGraphDescription{
		Kind: eachKeywordName,
		Name: "each.map",
		Form: compiler.ActionGraphOpaque,
	}
}

func (e *EachExpr) Analyze(resolver compiler.CapabilityResolver) error {
	if e.Body == nil {
		return compiler.SourceError(e.Pos, "each: block must contain a Flow expression")
	}
	return e.Body.Analyze(resolver)
}

func (e *EachExpr) AnalyzeTypes(ctx compiler.TypeAnalysisContext) (compiler.TypeDescriptor, error) {
	var elementType reflect.Type
	inputType := ctx.Input.Type
	for inputType != nil && inputType.Kind() == reflect.Pointer {
		inputType = inputType.Elem()
	}
	if inputType != nil {
		kind := inputType.Kind()
		if kind == reflect.Slice || kind == reflect.Array {
			elementType = inputType.Elem()
		} else if kind != reflect.Interface {
			return compiler.TypeDescriptor{}, compiler.SourceError(e.Pos,
				"each: input must be a list, got %s", ctx.Input.Type)
		}
		// Interface values are checked at runtime; they may carry a list.
	}

	childInput := compiler.TypeDescriptor{}
	if elementType != nil && elementType.Kind() != reflect.Interface {
		childInput.Type = elementType
	}
	childContext := compiler.TypeAnalysisContext{
		Resolver:           ctx.Resolver,
		Input:              childInput,
		OriginalInput:      ctx.OriginalInput,
		Bindings:           ctx.Bindings,
		PipelineBindings:   ctx.PipelineBindings,
		SymbolNamespaces:   ctx.SymbolNamespaces,
		CheckExpression:    ctx.CheckExpression,
		ExpressionCompiler: ctx.ExpressionCompiler,
	}
	if _, err := compiler.AnalyzeTypedExpression(childContext, e.Body); err != nil {
		return compiler.TypeDescriptor{}, err
	}
	return compiler.TypeDescriptor{Type: reflect.TypeFor[[]any]()}, nil
}

func (e *EachExpr) Build(ctx context.Context, bCtx *compiler.BuildContext) (action.AnyAction, error) {
	body, err := e.Body.Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}
	itemAction := action.New("each.item", func(itemCtx context.Context, item any) (any, error) {
		return action.InvokeAny(compiler.WithChildExpressionScope(itemCtx), body, item)
	}).Tag("control", "each_item").Build()

	program := action.New("each.map", func(runCtx context.Context, input any) (any, error) {
		items, err := eachItems(input)
		if err != nil {
			return nil, err
		}
		if !compiler.HasExpressionScope(runCtx) {
			runCtx = compiler.WithExpressionScope(runCtx, input)
		}
		return nodes_distribute.MapAction(runCtx, itemAction, items, e.Concurrency)
	}).Description("Map a Flow block over list items with bounded concurrency and ordered results").
		Tag("control", "each", "fanout").
		Build()

	composition := action.IntrospectionComposition{Kind: eachKeywordName}
	if bCtx.Introspect {
		composition.Opaque = true
		composition.Notes = []string{
			"block runs once per list item; results preserve input order",
			"item failures cancel sibling work and wait for launched actions to finish",
		}
		composition.Children = []action.IntrospectionChild{{
			Node: itemAction, Relation: "item", Role: "body", Label: "each item",
		}}
	}
	return compiler.TrackActionComposition(bCtx, program, composition), nil
}

func eachItems(input any) ([]any, error) {
	if items, ok := input.([]any); ok {
		if items == nil {
			return []any{}, nil
		}
		return items, nil
	}
	if input == nil {
		return nil, xerr.Validation("each: input must be a list, got nil")
	}
	value := reflect.ValueOf(input)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, xerr.Validation("each: input must be a list, got nil pointer")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return nil, xerr.Validation("each: input must be a list, got " + reflect.TypeOf(input).String())
	}
	if err := nodes_distribute.ValidateFanoutItemCount(value.Len()); err != nil {
		return nil, err
	}
	items := make([]any, value.Len())
	for i := range items {
		items[i] = value.Index(i).Interface()
	}
	return items, nil
}
