package lang

import (
	"encoding/json"
	"strings"
)

// Language is the runtime view of a runner spec. One per (language, version).
// Tags are gone by here — runner file aliases are applied at parse time,
// and from this struct onward every code path speaks the canonical ID
// ("python"), never the tag ("py").
//
// One struct, every language. New languages add a runners/<id>.nflow file,
// zero Go changes. See runners.go.
type Language struct {
	// ID is the canonical language identifier, e.g. "python".
	ID string

	// Ext is the file extension used when materializing source to disk,
	// including the leading dot, e.g. ".py".
	Ext string

	// Select resolves a version string to a runnable command. Select is
	// FIND-ONLY: it never installs. Resolve() is the gate that calls
	// Ensure on miss. Preflight uses Select (not Resolve) to detect missing
	// interpreters without triggering install.
	Select func(version string) (cmd string, args []string, err error)

	// Encode serializes the incoming stream value to bytes for stdin.
	// JSON is the default; shell/raw languages use text.
	Encode func(v any) ([]byte, error)

	// Decode parses stdout back into a typed value the next atom sees.
	// Takes string (not []byte) to avoid the []byte(string) copy from
	// external.ExecResult.Stdout. JSON is the default; shell/raw return string.
	Decode func(s string) (any, error)

	// spec is the parsed RunnerSpec. Used by Ensure for install recipes
	// and by preflight for reporting. Nil for languages registered via
	// RegisterDefine (@lang.define data model) — those have no install
	// recipe and rely on the user to install the interpreter.
	spec *RunnerSpec

	// install is the per-OS install recipe. Empty OSRecipe means
	// "no auto-install path; surface NotInstalledError with guidance."
	install OSRecipe
}

// Registry is the process-global table of known languages. Populated at
// init() by LoadRunners (from embedded runners/*.nflow) and at parse time
// by RegisterDefine (define.go) when @lang.define is used.
//
// Keyed by (ID, Version) — versioned variants of the same language
// (python "", python "2", python "3") are distinct entries. The
// unversioned Lookup(id, "") returns the default variant.
//
// Two languages with the same (ID, Version) panic — silent override would
// let a stale registration win and produce confusing runtime behavior.
var Registry = map[string]*Language{}

// registryKey returns the map key for an (ID, Version) pair.
// "python" with version "" -> "python"
// "python" with version "3" -> "python@3"
func registryKey(id, version string) string {
	if version == "" {
		return id
	}
	return id + "@" + version
}

// Register adds a language to the Registry. Duplicate (ID, Version) panic,
// matching the project rule: "collisions panic; missing contracts fail
// loudly."
func Register(l Language) {
	if l.ID == "" {
		panic("lang: Language.ID must be non-empty")
	}
	key := registryKey(l.ID, l.Version())
	if _, dup := Registry[key]; dup {
		panic("lang: language " + key + " already registered")
	}
	Registry[key] = &l
}

// Lookup returns the registered language for an (ID, Version) pair. The
// exact version is matched — Lookup("python", "3") returns the v3 variant,
// Lookup("python", "") returns the default.
func Lookup(id, version string) (*Language, error) {
	l, ok := Registry[registryKey(id, version)]
	if !ok {
		return nil, &UnknownLanguageError{ID: id, Version: version}
	}
	return l, nil
}

// UnknownLanguageError is returned by Lookup and ParseTag when the alias
// resolves to an (ID, Version) pair the Registry has no entry for. It
// surfaces as a compile-time SourceError via the parser hook.
type UnknownLanguageError struct {
	ID, Version string
}

func (e *UnknownLanguageError) Error() string {
	if e.Version == "" {
		return "unknown language: " + e.ID
	}
	return "unknown language: " + e.ID + "@" + e.Version
}

// Version returns the language's version string. Reads from spec.Version
// when a spec is attached (the runner-file path); "" otherwise (the
// @lang.define path stores version on the spec too via RegisterDefine).
func (l *Language) Version() string {
	if l.spec != nil {
		return l.spec.Version
	}
	return ""
}

// --- shared codecs -------------------------------------------------------

// encodeJSON is the default stdin serializer. nil input becomes empty stdin,
// not the bytes "null", because most languages treat EOF on stdin as "no
// input" rather than "JSON null".
func encodeJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

// decodeJSON parses stdout as JSON. Empty stdout becomes nil rather than an
// EOF error — pipelines frequently end in a side-effecting print with no
// return value.
//
// Uses json.NewDecoder on a strings.Reader to avoid the []byte(string)
// copy that json.Unmarshal would require.
func decodeJSON(s string) (any, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // Empty output intentionally decodes to a nil value.
	}
	var v any
	if err := json.NewDecoder(strings.NewReader(s)).Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// encodeText is used by shell and "raw" codecs. Strings and byte slices pass
// through; structured values are JSON-encoded as a fallback so a flow that
// passes {"x": 1} into bash still produces parseable input.
func encodeText(v any) ([]byte, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []byte(t), nil
	case []byte:
		return t, nil
	default:
		return json.Marshal(v)
	}
}

// decodeText returns the raw stdout as a string. Used by shell and raw codecs.
// No allocation beyond the string header — stdout is already a string from
// external.ExecResult.
func decodeText(s string) (any, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // Empty output intentionally decodes to a nil value.
	}
	return s, nil
}
