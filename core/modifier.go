package core

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// Modifier is one `:name` or `:name=value` annotation on an atom.
//
// Name is the identifier without the leading colon. Apply runs once per
// occurrence, in source order, on the atom's builder. The raw string is
// the DSL text after `=`, or "" for a flag. The parser has already
// stripped surrounding quotes and expanded adjacent tokens, so no
// modifier needs to worry about either.
//
// Owner declares which subsystem owns the name. A modifier whose name
// is reserved by the Kernel or the grammar must be declared by its
// owner; NewModifierTable panics on a mismatch.
type Modifier struct {
	Name    string
	Owner   ModifierOwner
	Example string
	Apply   func(b *action.Builder[any, any], raw string) error
}

// WithOwner returns m with the given owner. The Kernel policy bundle
// uses it to declare its modifiers as OwnerKernel without a
// package-level constructor for every policy.
func WithOwner(m Modifier, owner ModifierOwner) Modifier {
	m.Owner = owner
	return m
}

// ModifierTable indexes modifiers by name. Construction is eager: a
// duplicate name, or a reserved name declared by the wrong owner,
// panics.
type ModifierTable struct {
	byName  map[string]Modifier
	ordered []Modifier
}

func NewModifierTable(modifiers ...Modifier) *ModifierTable {
	table := &ModifierTable{
		byName:  make(map[string]Modifier, len(modifiers)),
		ordered: append([]Modifier(nil), modifiers...),
	}
	for _, modifier := range modifiers {
		if modifier.Name == "" {
			panic("core: modifier with empty name")
		}
		if _, duplicate := table.byName[modifier.Name]; duplicate {
			panic("core: duplicate modifier " + modifier.Name)
		}
		if required := RequiredModifierOwner(modifier.Name); required != OwnerBundle && modifier.Owner != required {
			panic("core: modifier " + modifier.Name + " is reserved for " + ModifierOwnerName(required))
		}
		table.byName[modifier.Name] = modifier
	}
	return table
}

func (t *ModifierTable) ByName(name string) (Modifier, bool) {
	if t == nil {
		return Modifier{}, false
	}
	modifier, ok := t.byName[name]
	return modifier, ok
}

func (t *ModifierTable) All() []Modifier {
	if t == nil {
		return nil
	}
	return append([]Modifier(nil), t.ordered...)
}

// ApplyAll wraps target once, applies every modifier in source order, and
// builds once. Two allocations total: the Builder and the BuiltAction.
//
// When a modifier name is unknown, the hint path checks whether the
// name matches a field of the action's request struct. That is the
// common DSL-author mistake — writing :file=... where the action takes
// @{ file: ... } — and it produces a fix-the-typo message instead of a
// bare "unknown modifier".
func (t *ModifierTable) ApplyAll(target action.AnyAction, raws []string) (action.AnyAction, error) {
	if len(raws) == 0 {
		return target, nil
	}
	builder := action.Dynamic(target)
	for _, raw := range raws {
		name, value := splitRawModifier(raw)
		modifier, ok := t.ByName(name)
		if !ok {
			if hint := SuggestModifierFix(target, name); hint != "" {
				return nil, xerr.Validation(hint)
			}
			return nil, xerr.BadRequest("unknown modifier :" + name)
		}
		if err := modifier.Apply(builder, value); err != nil {
			return nil, xerr.Validation("modifier :"+name+": "+err.Error(), err)
		}
	}
	return builder.Build(), nil
}

func (t *ModifierTable) Actions() []action.AnyAction { return nil }

func splitRawModifier(raw string) (name, value string) {
	if index := strings.IndexByte(raw, '='); index > 0 {
		return raw[:index], raw[index+1:]
	}
	return raw, ""
}

// ── Typed constructors ──────────────────────────────────────────────
//
// Every modifier in the ecosystem parses its DSL text through one of
// these constructors. No extension, no standard modifier, and no future
// bundle should call time.ParseDuration, strconv.*, or strings.Split on
// a modifier payload directly. The parsing rules for `:name=value` live
// here and nowhere else.
//
// The typed callback returns the builder so method expressions from
// *action.Builder — e.g. (*action.Builder[any, any]).Timeout — can be
// passed directly. The return value is discarded; the builder mutates
// in place. Callers writing a lambda must return the builder as the
// final statement.

func Duration(name string, apply func(*action.Builder[any, any], time.Duration) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name: name,
		Apply: func(b *action.Builder[any, any], raw string) error {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("parse duration: %w", err)
			}
			apply(b, d)
			return nil
		},
	}
}

func Int(name string, apply func(*action.Builder[any, any], int) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name: name,
		Apply: func(b *action.Builder[any, any], raw string) error {
			n, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("parse int: %w", err)
			}
			apply(b, n)
			return nil
		},
	}
}

func Int32(name string, apply func(*action.Builder[any, any], int32) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name: name,
		Apply: func(b *action.Builder[any, any], raw string) error {
			n, err := strconv.ParseInt(raw, 10, 32)
			if err != nil {
				return fmt.Errorf("parse int32: %w", err)
			}
			apply(b, int32(n))
			return nil
		},
	}
}

func Int64(name string, apply func(*action.Builder[any, any], int64) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name: name,
		Apply: func(b *action.Builder[any, any], raw string) error {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return fmt.Errorf("parse int64: %w", err)
			}
			apply(b, n)
			return nil
		},
	}
}

func Float64(name string, apply func(*action.Builder[any, any], float64) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name: name,
		Apply: func(b *action.Builder[any, any], raw string) error {
			f, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return fmt.Errorf("parse float64: %w", err)
			}
			apply(b, f)
			return nil
		},
	}
}

func String(name string, apply func(*action.Builder[any, any], string) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name: name,
		Apply: func(b *action.Builder[any, any], raw string) error {
			apply(b, raw)
			return nil
		},
	}
}

func StringList(name string, apply func(*action.Builder[any, any], []string) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name: name,
		Apply: func(b *action.Builder[any, any], raw string) error {
			items := splitListValue(raw)
			if len(items) == 0 {
				return errors.New("expected at least one value")
			}
			apply(b, items)
			return nil
		},
	}
}

// Flag handles modifiers that take no value (`:coalesce`, `:dedup`).
// Passing a value is rejected loudly — `:dedup=yes` in the DSL is a
// mistake the author should see, not silently accept.
func Flag(name string, apply func(*action.Builder[any, any]) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name: name,
		Apply: func(b *action.Builder[any, any], raw string) error {
			if raw != "" {
				return fmt.Errorf("flag does not take a value, got %q", raw)
			}
			apply(b)
			return nil
		},
	}
}

func splitListValue(raw string) []string {
	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// ModifiersToMap parses a slice of raw modifier strings (e.g. "ext=txt",
// "by=rel_path", "tests") into a map of parameters for stream sources
// and operators.
func ModifiersToMap(modifiers []string) map[string]any {
	if len(modifiers) == 0 {
		return nil
	}
	out := make(map[string]any, len(modifiers))
	for _, m := range modifiers {
		eq := strings.IndexByte(m, '=')
		if eq > 0 {
			k := strings.ToLower(strings.TrimSpace(m[:eq]))
			v := strings.Trim(strings.TrimSpace(m[eq+1:]), `"'`)
			out[k] = v
		} else if trimmed := strings.TrimSpace(m); trimmed != "" {
			out[strings.ToLower(trimmed)] = true
		}
	}
	return out
}
