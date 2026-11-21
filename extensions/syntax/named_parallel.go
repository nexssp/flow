package syntax

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

const parallelKeyword = "parallel"

type namedParallelKeyword struct{}

func (namedParallelKeyword) Name() string    { return "named_parallel" }
func (namedParallelKeyword) Keyword() string { return parallelKeyword }

func (namedParallelKeyword) Parse(p *compiler.Parser) (compiler.Expr, error) {
	p.Advance() // consume "parallel"
	if p.Current().Type != compiler.TokLBrace {
		return nil, p.Fail(p.Current().Line, "expected '{' after parallel")
	}
	p.Advance()

	if p.Current().Type == compiler.TokRBrace {
		return nil, p.Fail(p.Current().Line, "parallel block must contain at least one branch")
	}

	branches := make([]namedParallelBranch, 0, 2)
	seen := make(map[string]struct{})
	for {
		labelToken := p.Current()
		if labelToken.Type != compiler.TokIdent {
			return nil, p.Fail(labelToken.Line, "expected parallel branch label")
		}
		label := labelToken.Lit
		if _, exists := seen[label]; exists {
			return nil, p.Fail(labelToken.Line, "duplicate parallel branch label %q", label)
		}
		seen[label] = struct{}{}
		p.Advance()

		if p.Current().Type != compiler.TokColon {
			return nil, p.Fail(p.Current().Line, "expected ':' after parallel branch label %q", label)
		}
		p.Advance()

		branchExpr, err := p.ParseExpr(0)
		if err != nil {
			return nil, err
		}
		branches = append(branches, namedParallelBranch{label: label, expr: branchExpr})

		switch p.Current().Type {
		case compiler.TokComma:
			p.Advance()
			if p.Current().Type == compiler.TokRBrace {
				p.Advance()
				return &NamedParallelExpr{branches: branches}, nil
			}
		case compiler.TokInvalid:
			return nil, p.Fail(p.Current().Line, "unexpected character %q", p.Current().Lit)
		case compiler.TokRBrace:
			p.Advance()
			return &NamedParallelExpr{branches: branches}, nil
		case compiler.TokEOF, compiler.TokIdent, compiler.TokString, compiler.TokNumber,
			compiler.TokArrow, compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr,
			compiler.TokLParen, compiler.TokRParen, compiler.TokLBrace, compiler.TokLBracket,
			compiler.TokRBracket, compiler.TokColon,
			compiler.TokEquals, compiler.TokQuestion, compiler.TokHash, compiler.TokTilde,
			compiler.TokAtBrace, compiler.TokAtPrompt:
			return nil, p.Fail(p.Current().Line, "expected ',' or '}' after parallel branch %q", label)
		}
	}
}

type namedParallelBranch struct {
	label string
	expr  compiler.Expr
}

type namedParallelTask struct {
	index int
	input any
}

type namedParallelActionBranch struct {
	label  string
	action action.AnyAction
}

// NamedParallelExpr runs expressions concurrently and returns results keyed by
// the explicit labels in its source block.
type NamedParallelExpr struct {
	branches []namedParallelBranch
}

func (*NamedParallelExpr) Node() {}

func (e *NamedParallelExpr) Children() []compiler.Expr {
	children := make([]compiler.Expr, len(e.branches))
	for i, branch := range e.branches {
		children[i] = branch.expr
	}
	return children
}

func (e *NamedParallelExpr) DescribeActionGraph() compiler.ActionGraphDescription {
	labels := make([]string, len(e.branches))
	for i, branch := range e.branches {
		labels[i] = branch.label
	}
	return compiler.ActionGraphDescription{
		Kind:        parallelKeyword,
		Name:        parallelKeyword,
		Form:        compiler.ActionGraphParallel,
		ChildLabels: labels,
	}
}

func (e *NamedParallelExpr) Analyze(resolver compiler.CapabilityResolver) error {
	for _, branch := range e.branches {
		if err := branch.expr.Analyze(resolver); err != nil {
			return err
		}
	}
	return nil
}

func (e *NamedParallelExpr) AnalyzeTypes(ctx compiler.TypeAnalysisContext) (compiler.TypeDescriptor, error) {
	fields := make([]reflect.StructField, 0, len(e.branches))
	for index, branch := range e.branches {
		output, err := compiler.AnalyzeTypedExpression(ctx, branch.expr)
		if err != nil {
			return compiler.TypeDescriptor{}, err
		}
		fieldType := output.Type
		if fieldType == nil || output.Stream {
			fieldType = reflect.TypeFor[any]()
		}
		fields = append(fields, reflect.StructField{
			Name: "Branch" + strconv.Itoa(index),
			Type: fieldType,
			Tag: reflect.StructTag(
				"expr:" + strconv.Quote(branch.label) +
					" json:" + strconv.Quote(branch.label) +
					" flow_projection:\"true\"",
			),
		})
	}
	return compiler.TypeDescriptor{Type: namedParallelResultType(fields)}, nil
}

func namedParallelResultType(fields []reflect.StructField) (result reflect.Type) {
	defer func() {
		if recover() != nil {
			result = reflect.TypeFor[map[string]any]()
		}
	}()
	return reflect.StructOf(fields)
}

func (e *NamedParallelExpr) Build(ctx context.Context, bCtx *compiler.BuildContext) (action.AnyAction, error) {
	branches := make([]namedParallelActionBranch, 0, len(e.branches))
	var children []action.IntrospectionChild
	if bCtx.Introspect {
		children = make([]action.IntrospectionChild, 0, len(e.branches))
	}
	for _, branch := range e.branches {
		compiled, err := branch.expr.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
		compiled = compiler.WithChildExpressionScopeAction(compiled)
		branches = append(branches, namedParallelActionBranch{label: branch.label, action: compiled})
		if bCtx.Introspect {
			children = append(children, action.IntrospectionChild{
				Node: compiled, Relation: compiler.IntrospectionKindBranch, Role: "branch", Label: branch.label,
			})
		}
	}
	branchWorker := action.New("parallel.named.branch", func(execCtx context.Context, task namedParallelTask) (any, error) {
		branch := branches[task.index]
		var result any
		var branchErr error
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					branchErr = fmt.Errorf("panic in parallel branch [%s]: %v", branch.label, recovered)
				}
			}()
			result, branchErr = action.InvokeAny(execCtx, branch.action, task.input)
		}()
		return result, branchErr
	}).Build()

	programBuilder := action.New(parallelKeyword, func(execCtx context.Context, input any) (map[string]any, error) {
		tasks := make([]namedParallelTask, len(branches))
		for index := range branches {
			tasks[index] = namedParallelTask{index: index, input: input}
		}
		outcomes := action.FanOutWithOptions(execCtx, branchWorker, tasks, action.AsyncOptions{})
		results := make(map[string]any, len(branches))
		for index, outcome := range outcomes {
			if outcome.Err != nil {
				return results, fmt.Errorf("parallel branch [%s] failed: %w", branches[index].label, outcome.Err)
			}
			results[branches[index].label] = outcome.Value
		}
		return results, nil
	}).Description(fmt.Sprintf("Heterogeneous parallel execution of %d branches", len(branches)))
	tags := []string{"parallel", "named", "composer"}
	for _, branch := range branches {
		for _, tag := range branch.action.Describe().Tags {
			if !slices.Contains(tags, tag) {
				tags = append(tags, tag)
			}
		}
	}
	programBuilder.Tag(tags...)
	program := programBuilder.Build()
	return compiler.TrackActionComposition(bCtx, program, action.IntrospectionComposition{
		Kind: "parallel.named", Children: children,
	}), nil
}
