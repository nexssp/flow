// Package core holds the domain-agnostic parts of the directives system.
//
// Nothing here knows about any specific directive. Every shipped
// directive (at_route, at_retry, …) and every user-provided directive
// depends on core, never the other way around. The dependency graph is
// strictly: directive -> core -> compiler.
package core

// Directive is the interface every DSL directive implements.
//
// Apply consumes one or more lines starting at index i and returns the
// index of the first line after the directive. Most directives consume
// exactly one line (i+1). Block directives consume the header, the body
// between `{` and `}`, and the closing brace (see SplitBlock).
type Directive interface {
	Name() string
	Apply(ctx *Context, lines []string, i int) (next int, err error)
}

var (
	registry []Directive
	byName   = map[string]Directive{}
)

// Register adds a directive to the global registry. Called from each
// directive's init(). Duplicate names panic — this is a programming
// error, not a runtime condition.
func Register(d Directive) {
	if d == nil {
		panic("directives: Register(nil)")
	}
	name := d.Name()
	if name == "" {
		panic("directives: directive with empty Name()")
	}
	if _, dup := byName[name]; dup {
		panic("directives: duplicate directive @" + name)
	}
	byName[name] = d
	registry = append(registry, d)
}

// Lookup finds the directive whose name matches the token immediately
// after `@` on the given line. Returns false when the line does not
// start with `@` or the name is not registered.
func Lookup(line string) (Directive, bool) {
	if len(line) < 2 || line[0] != '@' {
		return nil, false
	}
	rest := line[1:]
	end := len(rest)
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case ' ', '\t', ':', '{':
			end = i
		}
		if end != len(rest) {
			break
		}
	}
	d, ok := byName[rest[:end]]
	return d, ok
}

// All returns a copy of the registry in registration order.
func All() []Directive {
	out := make([]Directive, len(registry))
	copy(out, registry)
	return out
}

// Names returns the registered directive names in registration order.
func Names() []string {
	out := make([]string, 0, len(registry))
	for _, d := range registry {
		out = append(out, d.Name())
	}
	return out
}
