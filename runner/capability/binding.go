package capability

import (
	"strings"
)

// Kind identifies the resolution backend for a declared capability.
type Kind uint8

const (
	KindHTTP Kind = iota + 1 // JSON over HTTP POST
	KindExec                 // subprocess: stdin JSON -> stdout JSON
	KindWASM                 // wazero module: stdin JSON -> stdout JSON
)

func (k Kind) String() string {
	switch k {
	case KindHTTP:
		return "http"
	case KindExec:
		return "exec"
	case KindWASM:
		return "wasm"
	default:
		return "unknown"
	}
}

// Binding is a single manifest-declared capability. Name is the node
// identifier used in the pipeline (e.g. "agent.orchestrator"); the remaining
// fields describe how to reach it.
type Binding struct {
	Name    string
	Kind    Kind
	Target  string // URL, shell command, or wasm file path
	Timeout string // raw duration string; parsed by the resolver
}

// ParseBindings scans a raw .flow manifest for capability declarations.
//
// Recognised modifiers:
//
//	:remote="http://host:port/path"   -> HTTP binding
//	:exec="cmd arg1 arg2"             -> exec binding
//	:wasm="./skills/foo.wasm"         -> WASM binding
//	:timeout="30s"                    -> per-binding timeout hint
//
// Only the tail atom of each pipeline line is examined; head atoms inherit
// their bindings from their own declaration lines. Duplicate names are
// resolved last-wins.
func ParseBindings(manifest string) []Binding {
	byName := make(map[string]Binding)
	order := make([]string, 0)

	for _, rawLine := range strings.Split(manifest, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" ||
			strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "@") {
			continue
		}

		atom := tailAtom(trimmed)
		if atom == "" {
			continue
		}

		name, mods := splitAtom(atom)
		if name == "" || len(mods) == 0 {
			continue
		}

		b, ok := bindingFromMods(name, mods)
		if !ok {
			continue
		}

		if _, exists := byName[b.Name]; !exists {
			order = append(order, b.Name)
		}

		byName[b.Name] = b
	}

	out := make([]Binding, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}

	return out
}

// tailAtom returns the last pipeline atom from a line, ignoring arrow tails
// inside quoted strings and balanced braces/parens.
func tailAtom(line string) string {
	arrows := topLevelArrows(line)
	if len(arrows) == 0 {
		return line
	}

	return strings.TrimSpace(line[arrows[len(arrows)-1]+2:])
}

// topLevelArrows returns byte offsets of "->" operators not inside quotes,
// braces, or parens.
func topLevelArrows(line string) []int {
	var positions []int

	depth := 0
	inQuotes := false

	var quoteCh byte

	for i := 0; i < len(line)-1; i++ {
		ch := line[i]
		if inQuotes {
			if ch == '\\' && i+1 < len(line) {
				i++

				continue
			}

			if ch == quoteCh {
				inQuotes = false
			}

			continue
		}

		switch ch {
		case '"', '\'', '`':
			inQuotes = true
			quoteCh = ch
		case '{', '(':
			depth++
		case '}', ')':
			depth--
		case '-':
			if depth == 0 && line[i+1] == '>' {
				positions = append(positions, i)
			}
		}
	}

	return positions
}

// splitAtom separates "name:mod1:mod2" into ("name", []string{"mod1","mod2"}).
// The trailing @-annotation (if any) is stripped first.
func splitAtom(atom string) (string, []string) {
	atom = stripAnnotation(atom)

	colon := strings.IndexByte(atom, ':')
	if colon < 0 {
		return strings.TrimSpace(atom), nil
	}

	name := strings.TrimSpace(atom[:colon])
	mods := splitMods(atom[colon+1:])

	return name, mods
}

// splitMods splits `a=1:b="x:y"` into ["a=1", `b="x:y"`], respecting quotes.
func splitMods(s string) []string {
	var (
		out []string
		sb  strings.Builder
	)

	inQuotes := false

	var quoteCh byte

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuotes {
			sb.WriteByte(ch)

			if ch == '\\' && i+1 < len(s) {
				sb.WriteByte(s[i+1])
				i++

				continue
			}

			if ch == quoteCh {
				inQuotes = false
			}

			continue
		}

		switch ch {
		case '"', '\'', '`':
			inQuotes = true
			quoteCh = ch
			sb.WriteByte(ch)
		case ':':
			if sb.Len() > 0 {
				out = append(out, sb.String())
				sb.Reset()
			}
		default:
			sb.WriteByte(ch)
		}
	}

	if sb.Len() > 0 {
		out = append(out, sb.String())
	}

	return out
}

// stripAnnotation removes anything from the first unquoted '@'.
func stripAnnotation(s string) string {
	inQuotes := false

	var quoteCh byte

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuotes {
			if ch == '\\' && i+1 < len(s) {
				i++

				continue
			}

			if ch == quoteCh {
				inQuotes = false
			}

			continue
		}

		switch ch {
		case '"', '\'', '`':
			inQuotes = true
			quoteCh = ch
		case '@':
			return strings.TrimSpace(s[:i])
		}
	}

	return s
}

// bindingFromMods walks the modifier list and produces a Binding when at
// least one remote/exec/wasm modifier is present.
func bindingFromMods(name string, mods []string) (Binding, bool) {
	b := Binding{Name: name}

	for _, m := range mods {
		eq := strings.IndexByte(m, '=')
		if eq < 0 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(m[:eq]))
		val := trimQuotes(strings.TrimSpace(m[eq+1:]))

		switch key {
		case "remote":
			if isHTTP(val) {
				b.Kind = KindHTTP
				b.Target = val
			}
		case "exec":
			b.Kind = KindExec
			b.Target = val
		case "wasm":
			b.Kind = KindWASM
			b.Target = val
		case "timeout":
			b.Timeout = val
		}
	}

	return b, b.Target != ""
}

func isHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') ||
			(s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}

	return s
}
