package core

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/nexssp/kernel/action"
)

// Catalog is a machine-readable dump of the compiler surface: every
// atom, stream source, operator, modifier, and directive known to the
// resolver and tables. Consumed by `nflow catalog`, `nflow list`,
// `nflow show`, and the linter. Regenerated on every build — no manual
// maintenance.
type Catalog struct {
	Atoms      []AtomSpec      `json:"atoms"`
	Sources    []SourceSpec    `json:"sources"`
	Operators  []OperatorSpec  `json:"operators"`
	Modifiers  []ModifierSpec  `json:"modifiers"`
	Directives []DirectiveSpec `json:"directives"`
}

type AtomSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Scope       string          `json:"scope,omitempty"`
	ReqFields   []FieldSpec     `json:"req_fields,omitempty"`
	ResFields   []FieldSpec     `json:"res_fields,omitempty"`
	Example     json.RawMessage `json:"example,omitempty"`
}

// SourceSpec describes a stream source (fs.walk, cov.items, ...). A
// source has a typed request but no typed item — the item type is a
// per-item element, not a whole response.
type SourceSpec struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	ReqFields   []FieldSpec `json:"req_fields,omitempty"`
}

type OperatorSpec struct {
	Name        string `json:"name"`
	Token       string `json:"token,omitempty"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
}

type ModifierSpec struct {
	Name    string `json:"name"`
	Example string `json:"example,omitempty"`
}

type DirectiveSpec struct {
	Name    string `json:"name"`
	Example string `json:"example,omitempty"`
}

type FieldSpec struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
	Usage    string `json:"usage,omitempty"`
}

// BuildCatalog extracts the full compiler surface from the live
// resolver and tables. Atom fields come from TypedPayload when the
// atom is a *BuiltAction[Req, Res]; free-form actions show no fields.
func BuildCatalog(
	resolver CapabilityResolver,
	mt *ModifierTable,
	dt *DirectiveTable,
	ot *OperatorTable,
) Catalog {
	cat := Catalog{}

	cat.Atoms = collectAtoms(resolver)
	cat.Sources = collectSources(resolver)
	cat.Operators = collectOperators(resolver, ot)
	cat.Modifiers = collectModifiers(mt)
	cat.Directives = collectDirectives(dt)

	return cat
}

func collectAtoms(resolver CapabilityResolver) []AtomSpec {
	r, ok := resolver.(interface{ Actions() []action.AnyAction })
	if !ok {
		return nil
	}

	out := make([]AtomSpec, 0, 32)
	for _, act := range r.Actions() {
		meta := act.Describe()
		if meta == nil {
			continue
		}

		spec := AtomSpec{
			Name:        meta.Name,
			Description: meta.Description,
			Tags:        meta.Tags,
			Scope:       string(meta.Scope),
		}
		if spec.Scope == "" {
			spec.Scope = "public"
		}
		if meta.Example != nil {
			if raw, err := json.Marshal(meta.Example); err == nil {
				spec.Example = raw
			}
		}
		if typed, ok := act.(action.TypedPayload); ok {
			spec.ReqFields = fieldsOf(typed.ReqPayload())
			spec.ResFields = fieldsOf(typed.ResPayload())
		}

		out = append(out, spec)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// collectSources reads the stream sources mounted on the resolver.
// The resolver interface does not declare a Streams() accessor, so the
// type assertion targets DynamicResolver's concrete method through an
// anonymous interface. Callers using a custom resolver that does not
// implement Streams simply get an empty Sources section.
func collectSources(resolver CapabilityResolver) []SourceSpec {
	r, ok := resolver.(interface {
		Streams() []action.AnyStreamAction
	})
	if !ok {
		return nil
	}

	streams := r.Streams()

	out := make([]SourceSpec, 0, len(streams))
	for _, src := range streams {
		if src == nil {
			continue
		}
		meta := src.Describe()
		if meta == nil {
			continue
		}

		spec := SourceSpec{
			Name:        meta.Name,
			Description: meta.Description,
			Tags:        meta.Tags,
		}
		if typed, ok := src.(action.TypedPayload); ok {
			spec.ReqFields = fieldsOf(typed.ReqPayload())
		}

		out = append(out, spec)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// collectOperators merges two sources of operator metadata:
//
//   - the OperatorTable, which owns the compiler-syntax operators
//     (pipe, parallel, fallback) with their precedence and token;
//   - the resolver, which owns the stream operators (fs.filter,
//     render.markdown, ...) mounted by libraries.
//
// Names cannot collide: the table holds DSL syntax, the resolver holds
// runtime stream transforms. If they ever do collide, the table entry
// wins because it carries more metadata.
func collectOperators(resolver CapabilityResolver, ot *OperatorTable) []OperatorSpec {
	seen := make(map[string]struct{}, 16)
	out := make([]OperatorSpec, 0, 16)

	for _, op := range ot.All() {
		out = append(out, OperatorSpec{
			Name:        op.Name,
			Token:       op.Token.String(),
			Description: op.Description,
			Example:     op.Example,
		})
		seen[op.Name] = struct{}{}
	}

	if r, ok := resolver.(interface{ Operators() []action.NamedOperator }); ok {
		for _, op := range r.Operators() {
			if _, dup := seen[op.Name]; dup {
				continue
			}
			out = append(out, OperatorSpec{
				Name:        op.Name,
				Description: op.Description,
			})
			seen[op.Name] = struct{}{}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

func collectModifiers(mt *ModifierTable) []ModifierSpec {
	if mt == nil {
		return nil
	}

	all := mt.All()

	out := make([]ModifierSpec, 0, len(all))
	for _, m := range all {
		out = append(out, ModifierSpec{Name: m.Name, Example: m.Example})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

func collectDirectives(dt *DirectiveTable) []DirectiveSpec {
	if dt == nil {
		return nil
	}

	all := dt.All()

	out := make([]DirectiveSpec, 0, len(all))
	for _, d := range all {
		out = append(out, DirectiveSpec{
			Name:    d.Name,
			Example: d.Example,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// fieldsOf returns one FieldSpec per exported field of payload,
// including fields promoted from anonymous embedded structs. This is
// what makes `nflow show bench.save` list the BenchRunRes fields
// (action, iterations, p50_ms, ...) alongside the explicit File.
func fieldsOf(payload any) []FieldSpec {
	if payload == nil {
		return nil
	}

	t := reflect.TypeOf(payload)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var out []FieldSpec
	collectFields(t, &out)
	return out
}

func collectFields(t reflect.Type, out *[]FieldSpec) {
	for field := range t.Fields() {
		if !field.IsExported() {
			continue
		}

		tag := field.Tag.Get("json")
		tagName, _, _ := strings.Cut(tag, ",")

		// Anonymous embedded struct with no JSON name: its fields are
		// promoted into the parent object, so recurse instead of
		// listing the embedding itself.
		if field.Anonymous && tagName == "" && field.Type.Kind() == reflect.Struct {
			collectFields(field.Type, out)
			continue
		}
		if tagName == "-" {
			continue
		}
		if tagName == "" {
			tagName = field.Name
		}

		*out = append(*out, FieldSpec{
			Name:     tagName,
			Type:     field.Type.String(),
			Required: strings.Contains(field.Tag.Get("validate"), "required"),
			Usage:    field.Tag.Get("usage"),
		})
	}
}
