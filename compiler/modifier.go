package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// ModifierKind describes the compiler-level value shape of a modifier.
// The typed constructors (Duration, Int, Flag, ...) set it
// automatically, so most code never references it directly.
type ModifierKind uint8

const (
	// ModifierKindAny accepts any value. It is the zero value, so a
	// modifier built via a raw struct literal (not a typed constructor)
	// performs no value validation.
	ModifierKindAny ModifierKind = iota
	ModifierKindFlag
	ModifierKindString
	ModifierKindStringList
	ModifierKindInt
	ModifierKindInt32
	ModifierKindInt64
	ModifierKindFloat64
	ModifierKindDuration
	ModifierKindJSON
)

// Modifier is one `:name` or `:name=value` annotation on an atom.
//
// Name is the identifier without the leading colon. The configured Apply
// or ApplyTarget callback runs once per occurrence, in source order. The
// raw string is the DSL text after `=`, or "" for a flag. The parser has already
// stripped surrounding quotes and expanded adjacent tokens, so no
// modifier needs to worry about either.
//
// Owner declares which subsystem owns the name. A modifier whose name
// is reserved by the Kernel or the grammar must be declared by its
// owner; NewModifierTable panics on a mismatch.
type Modifier struct {
	Name        string
	Owner       ModifierOwner
	ValueKind   ModifierKind
	Unique      bool
	Inheritable bool
	Example     string
	Apply       func(b *action.Builder[any, any], raw string) error
	// ApplyNative consumes a symbol value already parsed according to its
	// explicit @symbols type; it must not parse or stringify the value.
	ApplyNative func(b *action.Builder[any, any], value any) error
	// ApplyTarget receives the original action as well as its dynamic builder.
	// Use it for compile-time annotations that validate against request schema.
	ApplyTarget func(target action.AnyAction, b *action.Builder[any, any], raw string) error
}

// ModifierValue preserves the source spelling while optionally carrying a
// native, explicitly typed symbol value. Raw remains available for diagnostics
// and explain tooling.
type ModifierValue struct {
	Raw    string
	Value  any
	Typed  bool
	Quoted bool
}

// String returns a stable identifier for the kind, suitable for JSON
// catalogs, lint messages, and CLI output.
func (k ModifierKind) String() string {
	switch k {
	case ModifierKindAny:
		return "any"
	case ModifierKindFlag:
		return "flag"
	case ModifierKindString:
		return "string"
	case ModifierKindStringList:
		return "string_list"
	case ModifierKindInt:
		return "int"
	case ModifierKindInt32:
		return "int32"
	case ModifierKindInt64:
		return "int64"
	case ModifierKindFloat64:
		return "float64"
	case ModifierKindDuration:
		return "duration"
	case ModifierKindJSON:
		return "json"
	default:
		return "unknown"
	}
}

// WithOwner returns m with the given owner. The Kernel policy bundle
// uses it to declare its modifiers as OwnerKernel without a
// package-level constructor for every policy.
func WithOwner(m Modifier, owner ModifierOwner) Modifier {
	m.Owner = owner
	return m
}

// WithUnique marks a modifier as single-use: applying it more than once
// to the same atom is a compile-time error.
func WithUnique(m Modifier) Modifier {
	m.Unique = true
	return m
}

// WithInheritable marks a modifier as eligible for scope inheritance.
// @scope and @pipeline propagate only inheritable modifiers to nested
// atoms; metadata modifiers (tags, status, route) stay on the wrapper
// they were declared on.
func WithInheritable(m Modifier) Modifier {
	m.Inheritable = true
	return m
}

// ValidateModifierValue checks that value matches the modifier's kind.
// It does not run Apply; callers that mutate a builder use it to fail
// fast with a stable message before invoking the handler.
func ValidateModifierValue(kind ModifierKind, value string) error {
	switch kind {
	case ModifierKindAny:
		return nil
	case ModifierKindFlag:
		if value != "" {
			return fmt.Errorf("flag does not take a value, got %q", value)
		}
	case ModifierKindString:
		// Any string is valid.
	case ModifierKindJSON:
		if !json.Valid([]byte(value)) {
			return errors.New("expected valid JSON")
		}
	case ModifierKindStringList:
		if strings.TrimSpace(value) == "" {
			return errors.New("expected at least one value")
		}
	case ModifierKindInt:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("expected integer, got %q", value)
		}
	case ModifierKindInt32:
		if _, err := strconv.ParseInt(value, 10, 32); err != nil {
			return fmt.Errorf("expected int32, got %q", value)
		}
	case ModifierKindInt64:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("expected int64, got %q", value)
		}
	case ModifierKindFloat64:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("expected number, got %q", value)
		}
	case ModifierKindDuration:
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("expected duration (e.g. 5s, 1m), got %q", value)
		}
	}
	return nil
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
	values := make([]ModifierValue, len(raws))
	for i, raw := range raws {
		values[i] = ModifierValue{Raw: raw}
	}
	return t.ApplyAllValues(target, values)
}

// ApplyAllValues applies modifiers while preserving already-parsed native
// symbol values. Typed values are validated against the modifier's exact
// native kind before its ApplyNative callback is invoked.
func (t *ModifierTable) ApplyAllValues(target action.AnyAction, values []ModifierValue) (action.AnyAction, error) {
	if len(values) == 0 {
		return target, nil
	}
	builder := action.Dynamic(target)
	for i, input := range values {
		name, rawValue := splitRawModifier(input.Raw)
		modifier, ok := t.ByName(name)
		if !ok {
			if hint := SuggestModifierFix(target, name); hint != "" {
				return nil, xerr.Validation(hint)
			}
			return nil, xerr.BadRequest("unknown modifier :" + name)
		}
		if modifier.Unique {
			for j := range i {
				previous, _ := splitRawModifier(values[j].Raw)
				if previous == name {
					return nil, xerr.Validation("modifier :" + name + " is not repeatable")
				}
			}
		}

		var applyErr error
		if input.Typed {
			if err := validateNativeModifierValue(modifier.ValueKind, input.Value); err != nil {
				return nil, xerr.Validation("modifier :" + name + ": " + err.Error())
			}
			if modifier.ApplyNative == nil {
				return nil, xerr.Validation("modifier :" + name + ": does not accept typed symbol values")
			}
			applyErr = modifier.ApplyNative(builder, input.Value)
		} else {
			if err := ValidateModifierValue(modifier.ValueKind, rawValue); err != nil {
				return nil, xerr.Validation("modifier :" + name + ": " + err.Error())
			}
			switch {
			case modifier.ApplyTarget != nil:
				applyErr = modifier.ApplyTarget(target, builder, rawValue)
			case modifier.Apply != nil:
				applyErr = modifier.Apply(builder, rawValue)
			default:
				return nil, xerr.Internal("modifier :" + name + " has no application handler")
			}
		}
		if applyErr != nil {
			return nil, xerr.Validation("modifier :"+name+": "+applyErr.Error(), applyErr)
		}
	}
	return builder.Build(), nil
}

func validateNativeModifierValue(kind ModifierKind, value any) error {
	var ok bool
	switch kind {
	case ModifierKindAny:
		return nil
	case ModifierKindString:
		_, ok = value.(string)
	case ModifierKindInt:
		_, ok = value.(int)
	case ModifierKindInt32:
		_, ok = value.(int32)
	case ModifierKindInt64:
		_, ok = value.(int64)
	case ModifierKindFloat64:
		_, ok = value.(float64)
	case ModifierKindDuration:
		_, ok = value.(time.Duration)
	case ModifierKindFlag, ModifierKindStringList, ModifierKindJSON:
		return fmt.Errorf("typed symbol values are not supported for %s modifiers", kind)
	default:
		return fmt.Errorf("typed symbol values are not supported for %s modifiers", kind)
	}
	if !ok {
		return fmt.Errorf("expected %s typed value, got %T", kind, value)
	}
	return nil
}

func typedModifierValue[T any](kind ModifierKind, value any) (T, error) {
	native, ok := value.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("expected %s typed value, got %T", kind, value)
	}
	return native, nil
}

func (t *ModifierTable) Actions() []action.AnyAction { return nil }

func splitRawModifier(raw string) (name, value string) {
	if index := strings.IndexByte(raw, '='); index > 0 {
		return raw[:index], raw[index+1:]
	}
	return raw, ""
}

// ModifierName returns the name portion of a raw modifier string,
// before any '='.
func ModifierName(raw string) string {
	if i := strings.IndexByte(raw, '='); i > 0 {
		return raw[:i]
	}
	return raw
}

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
		Name:      name,
		ValueKind: ModifierKindDuration,
		Apply: func(b *action.Builder[any, any], raw string) error {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("parse duration: %w", err)
			}
			apply(b, d)
			return nil
		},
		ApplyNative: func(b *action.Builder[any, any], value any) error {
			native, err := typedModifierValue[time.Duration](ModifierKindDuration, value)
			if err != nil {
				return err
			}
			apply(b, native)
			return nil
		},
	}
}

func Int(name string, apply func(*action.Builder[any, any], int) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name:      name,
		ValueKind: ModifierKindInt,
		Apply: func(b *action.Builder[any, any], raw string) error {
			n, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("parse int: %w", err)
			}
			apply(b, n)
			return nil
		},
		ApplyNative: func(b *action.Builder[any, any], value any) error {
			native, err := typedModifierValue[int](ModifierKindInt, value)
			if err != nil {
				return err
			}
			apply(b, native)
			return nil
		},
	}
}

func Int32(name string, apply func(*action.Builder[any, any], int32) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name:      name,
		ValueKind: ModifierKindInt32,
		Apply: func(b *action.Builder[any, any], raw string) error {
			n, err := strconv.ParseInt(raw, 10, 32)
			if err != nil {
				return fmt.Errorf("parse int32: %w", err)
			}
			apply(b, int32(n))
			return nil
		},
		ApplyNative: func(b *action.Builder[any, any], value any) error {
			native, err := typedModifierValue[int32](ModifierKindInt32, value)
			if err != nil {
				return err
			}
			apply(b, native)
			return nil
		},
	}
}

func Int64(name string, apply func(*action.Builder[any, any], int64) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name:      name,
		ValueKind: ModifierKindInt64,
		Apply: func(b *action.Builder[any, any], raw string) error {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return fmt.Errorf("parse int64: %w", err)
			}
			apply(b, n)
			return nil
		},
		ApplyNative: func(b *action.Builder[any, any], value any) error {
			native, err := typedModifierValue[int64](ModifierKindInt64, value)
			if err != nil {
				return err
			}
			apply(b, native)
			return nil
		},
	}
}

func Float64(name string, apply func(*action.Builder[any, any], float64) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name:      name,
		ValueKind: ModifierKindFloat64,
		Apply: func(b *action.Builder[any, any], raw string) error {
			f, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return fmt.Errorf("parse float64: %w", err)
			}
			apply(b, f)
			return nil
		},
		ApplyNative: func(b *action.Builder[any, any], value any) error {
			native, err := typedModifierValue[float64](ModifierKindFloat64, value)
			if err != nil {
				return err
			}
			apply(b, native)
			return nil
		},
	}
}

func String(name string, apply func(*action.Builder[any, any], string) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name:      name,
		ValueKind: ModifierKindString,
		Apply: func(b *action.Builder[any, any], raw string) error {
			apply(b, raw)
			return nil
		},
		ApplyNative: func(b *action.Builder[any, any], value any) error {
			native, err := typedModifierValue[string](ModifierKindString, value)
			if err != nil {
				return err
			}
			apply(b, native)
			return nil
		},
	}
}

func StringList(name string, apply func(*action.Builder[any, any], []string) *action.Builder[any, any]) Modifier {
	return Modifier{
		Name:      name,
		ValueKind: ModifierKindStringList,
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
		Name:      name,
		ValueKind: ModifierKindFlag,
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

// filterInheritable returns a line-modifier lookup that keeps:
//
//   - modifiers declared Inheritable in mt, and
//   - modifiers unknown to mt.
//
// Unknown modifiers are passed through so ModifierTable.ApplyAll can
// reject them with its standard "unknown modifier" error. Only
// declared-but-not-inheritable modifiers are dropped — those are
// metadata (tag, status, route, …) and must not leak into body atoms.
//
// A nil table or nil inner function returns the lookup unchanged.
// Sources are preserved.
func filterInheritable(lk LineLookup, mt *ModifierTable) LineLookup {
	if mt == nil {
		return lk
	}
	keep := func(raw string) bool {
		mod, ok := mt.ByName(ModifierName(raw))
		return !ok || mod.Inheritable
	}
	if lk.Fn != nil {
		inner := lk.Fn
		lk.Fn = func(line int) []string {
			var kept []string
			for _, raw := range inner(line) {
				if keep(raw) {
					kept = append(kept, raw)
				}
			}
			return kept
		}
	}
	if lk.ValueFn != nil {
		inner := lk.ValueFn
		lk.ValueFn = func(line int) []ModifierValue {
			var kept []ModifierValue
			for _, value := range inner(line) {
				if keep(value.Raw) {
					kept = append(kept, value)
				}
			}
			return kept
		}
	}
	return lk
}
