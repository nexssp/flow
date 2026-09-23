package flow

import (
	"github.com/nexssp/flow/directives/builtin/at_schema"
	"github.com/nexssp/flow/directives/core"
)

func WithSchemas(pre *core.Preprocessed) CompileOption {
	return func(c *compileOptions) {
		decls := at_schema.SchemasFromPreprocessed(pre)
		if len(decls) == 0 {
			return
		}
		m := make(map[string]at_schema.Schema, len(decls))
		for _, s := range decls {
			m[s.Name] = s
		}
		c.schemas = m
	}
}
