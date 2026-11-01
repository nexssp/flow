package compiler

import (
	"context"
	"errors"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// ExpressionProgram is a compiled, source-local expression that can be evaluated
// against one input value. Extensions may implement it to bind compile-time
// functions without installing process-global expression functions.
type ExpressionProgram interface {
	Evaluate(context.Context, any) (any, error)
	EvaluateEnvironment(context.Context, map[string]any) (any, error)
}

// ExpressionCompiler compiles Flow expressions using one prepared source's
// declarations. The source and position are supplied by the caller; implementations
// must not retain request input or mutate shared registries.
type ExpressionCompiler interface {
	Compile(source string, position Position) (ExpressionProgram, error)
}

const expressionCompilerMetaKey = "compiler.expression.compiler"

// SetExpressionCompiler stores a source-local expression compiler in metadata.
// Extensions use this when other runtime facilities (such as post-run assertions)
// need the same compile-time bindings after the source has been prepared.
func SetExpressionCompiler(meta map[string]any, expressionCompiler ExpressionCompiler) {
	if meta == nil {
		return
	}
	if expressionCompiler == nil {
		delete(meta, expressionCompilerMetaKey)
		return
	}
	meta[expressionCompilerMetaKey] = expressionCompiler
}

// ExpressionCompilerFromMeta returns the expression compiler stored for one
// prepared source, or nil when that source has no expression extensions.
func ExpressionCompilerFromMeta(meta map[string]any) ExpressionCompiler {
	if meta == nil {
		return nil
	}
	expressionCompiler, _ := meta[expressionCompilerMetaKey].(ExpressionCompiler)
	return expressionCompiler
}

// CompileRuntimeExpression applies Flow's dotted-input syntax and compiles with
// the source-local extension when one is available. The default compiler uses
// expr-lang with undefined input fields allowed, matching projection semantics.
func CompileRuntimeExpression(source string, position Position, expressionCompiler ExpressionCompiler) (ExpressionProgram, error) {
	clean := PreprocessDots(source)
	if expressionCompiler != nil {
		program, err := expressionCompiler.Compile(clean, position)
		if err != nil {
			return nil, SourceError(position, "%v", err)
		}
		if program == nil {
			return nil, SourceError(position, "expression compiler returned a nil program")
		}
		return program, nil
	}
	program, err := CompileExpression(clean, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, SourceError(position, "%v", err)
	}
	return runtimeExpressionProgram{program: program}, nil
}

type runtimeExpressionProgram struct {
	program *vm.Program
}

func (p runtimeExpressionProgram) Evaluate(ctx context.Context, input any) (any, error) {
	return p.EvaluateEnvironment(ctx, BuildEnv(input))
}

func (p runtimeExpressionProgram) EvaluateEnvironment(ctx context.Context, environment map[string]any) (any, error) {
	if p.program == nil {
		return nil, errors.New("expression program is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	env := MergeSymbolNamespaces(ctx, environment)
	return expr.Run(p.program, env)
}

// CompileExpression compiles one runtime expression using this build's
// source-local expression compiler, falling back to Flow's default rules.
func (b *BuildContext) CompileExpression(source string, position Position) (ExpressionProgram, error) {
	if b == nil {
		return CompileRuntimeExpression(source, position, nil)
	}
	return CompileRuntimeExpression(source, position, b.ExpressionCompiler)
}
