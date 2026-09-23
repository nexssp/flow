package at_schema

import (
	"github.com/nexssp/flow/directives/core"
)

type directive struct{}

func init() {
	core.Register(directive{})
}

func (directive) Name() string { return "schema" }

func (directive) Apply(ctx *core.Context, lines []string, i int) (int, error) {
	header, body, next, err := core.SplitBlock(lines, i)
	if err != nil {
		return 0, core.AtErr(ctx, i, "schema", err.Error())
	}

	name, err := parseSchemaHeader(header)
	if err != nil {
		return 0, core.AtErr(ctx, i, "schema", err.Error())
	}

	schema, err := parseSchemaBody(name, body, ctx, i)
	if err != nil {
		return 0, err
	}

	existing, _ := ctx.Out.Declarations[SchemaDeclarationKey].([]Schema)
	for _, s := range existing {
		if s.Name == name {
			return 0, core.AtErrf(ctx, i, "schema "+name,
				"duplicate declaration (already at %s)", s.Pos)
		}
	}
	if ctx.Out.Declarations == nil {
		ctx.Out.Declarations = make(map[string]any)
	}
	ctx.Out.Declarations[SchemaDeclarationKey] = append(existing, schema)

	return next, nil
}
