package at_schema

import (
	"strings"

	"github.com/nexssp/flow/directives/core"
)

// SchemaKind classifies a schema field's scalar type. Composite shapes
// (slices, maps, pointers, references to other schemas) are expressed
// with the Slice/Map/Pointer flags and a Kind of the element type.
type SchemaKind uint8

const (
	KindString SchemaKind = iota
	KindInt
	KindFloat
	KindBool
	KindAny
	KindStruct
)

func (k SchemaKind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindInt:
		return "int"
	case KindFloat:
		return "float"
	case KindBool:
		return "bool"
	case KindAny:
		return "any"
	case KindStruct:
		return "struct"
	default:
		return "unknown"
	}
}

// SchemaField is one field inside a @schema declaration.
type SchemaField struct {
	Name     string
	JSONName string
	Type     string
	Kind     SchemaKind
	ElemType string
	Pointer  bool
	Slice    bool
	Map      bool
	Tags     map[string]string
	Pos      core.Position
}

// Schema is one parsed @schema block.
type Schema struct {
	Name   string
	Fields []SchemaField
	Pos    core.Position
}

// JSONExample returns a representative JSON-compatible map for the
// schema. Tooling (flow info, agent function specs) uses it to render
// an example request payload.
func (s Schema) JSONExample() map[string]any {
	out := make(map[string]any, len(s.Fields))
	for _, f := range s.Fields {
		out[f.JSONName] = f.exampleValue()
	}
	return out
}

func (f SchemaField) exampleValue() any {
	if f.Slice {
		return []any{}
	}
	if f.Pointer {
		return nil
	}
	switch f.Kind {
	case KindString:
		return ""
	case KindInt:
		return int64(0)
	case KindFloat:
		return 0.0
	case KindBool:
		return false
	case KindAny:
		return nil
	case KindStruct:
		return map[string]any{}
	default:
		return nil
	}
}

// SchemaDeclarationKey is the slot under Preprocessed.Declarations
// that holds every parsed @schema for a file.
const SchemaDeclarationKey = "schemas"

// SchemasFromPreprocessed returns every @schema declaration in source
// order, or nil when none were declared.
func SchemasFromPreprocessed(pre *core.Preprocessed) []Schema {
	if pre == nil || pre.Declarations == nil {
		return nil
	}
	schemas, _ := pre.Declarations[SchemaDeclarationKey].([]Schema)
	return schemas
}

// SchemaByName looks up a schema by its declared name.
func SchemaByName(pre *core.Preprocessed, name string) (Schema, bool) {
	for _, s := range SchemasFromPreprocessed(pre) {
		if s.Name == name {
			return s, true
		}
	}
	return Schema{}, false
}

// ── Classification helpers ───────────────────────────────────────────────────

func classifyScalarKind(t string) SchemaKind {
	switch t {
	case "string":
		return KindString
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return KindInt
	case "float32", "float64":
		return KindFloat
	case "bool":
		return KindBool
	case "any", "interface{}":
		return KindAny
	default:
		return KindStruct
	}
}

// isValidGoIdent reports whether s is a valid Go identifier starting
// with an uppercase letter. Schemas only accept exported fields.
func isValidGoIdent(s string) bool {
	if s == "" {
		return false
	}
	if c := s[0]; c < 'A' || c > 'Z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// lowerCamel converts "RepoPath" to "repoPath". Used as the JSON name
// when no `json:"…"` tag is present.
func lowerCamel(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 0 {
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			b.WriteByte(c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
