package lang

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"runtime"
	"strings"
)

// RunnerSpec is the data model parsed from a @lang.runner block in a .nflow
// file. One RunnerSpec per (language, version). The Go side is generic —
// adding a language means shipping a new runners/<id>.nflow file, zero Go
// changes.
//
// File format (one block per file, multiple files for versioned variants):
//
//	@lang.runner "python" {
//	    ext: ".py"
//	    aliases: ["py", "python"]
//	    find: ["python3", "python"]
//	    stdin: "json"
//	    stdout: "json"
//	    file_template: "..." // optional source scaffold for :autocreate
//	    install: {
//	        windows: ["winget", "install", "Python.Python.3.12"],
//	        darwin:  ["brew", "install", "python@3.12"]
//	    }
//	}
//
//	@lang.runner "python" "3" {
//	    "id": "python",
//	    "version": "3",
//	    "ext": ".py",
//	    "aliases": ["py3", "python3"],
//	    "find": ["python3", "py -3"],
//	    "stdin": "json",
//	    "stdout": "json"
//	}
//
// Fields:
//   - ID, Version: the canonical identity. Version "" means "default".
//   - Ext: file extension for materialized source, including the dot.
//   - Aliases: short keywords (py, python) that resolve to this spec.
//   - Find: candidate command lines tried in order. Each entry is a
//     space-split command + args; the first that resolves on PATH wins.
//   - Stdin, Stdout: "json" (default) or "text".
//   - FileTemplate: optional JSON-in/JSON-out starter source used by
//     `<alias>.file "path" :autocreate` when the target does not exist.
//   - Install: optional per-OS recipes for auto-install. Omit or leave
//     the OS entry empty to skip auto-install for that platform.
//   - Deps, DepsMarker: optional command template and source marker for
//     per-script dependency tools such as uv; {file} is passed as one arg.
type RunnerSpec struct {
	ID           string   `json:"id"`
	Version      string   `json:"version,omitempty"`
	Ext          string   `json:"ext"`
	Aliases      []string `json:"aliases,omitempty"`
	Find         []string `json:"find"`
	Stdin        string   `json:"stdin,omitempty"`
	Stdout       string   `json:"stdout,omitempty"`
	FileTemplate string   `json:"file_template,omitempty"`
	Install      OSRecipe `json:"install,omitzero"`
	Deps         string   `json:"deps,omitempty"`
	DepsMarker   string   `json:"deps_marker,omitempty"`
}

// OSRecipe holds per-OS install commands. Each entry is a full command
// line; the first element is the binary, the rest are args.
type OSRecipe struct {
	Windows []string `json:"windows,omitempty"`
	Darwin  []string `json:"darwin,omitempty"`
	Linux   []string `json:"linux,omitempty"`
}

// RecipeFor returns the install command for the current OS, or nil if no
// recipe is defined. Pure Go: callers run it via os/exec.
func (r OSRecipe) RecipeFor() []string {
	switch runtime.GOOS {
	case "windows":
		return r.Windows
	case "darwin":
		return r.Darwin
	case "linux":
		return r.Linux
	default:
		return nil
	}
}

// Encode resolves the stdin codec name to a function.
func (s RunnerSpec) Encode() (func(any) ([]byte, error), error) {
	switch s.Stdin {
	case "", "json":
		return encodeJSON, nil
	case "text", "raw":
		return encodeText, nil
	default:
		return nil, fmt.Errorf("%s: unknown stdin codec %q (want json|text|raw)", s.ID, s.Stdin)
	}
}

// Decode resolves the stdout codec name to a function.
func (s RunnerSpec) Decode() (func(string) (any, error), error) {
	switch s.Stdout {
	case "", "json":
		return decodeJSON, nil
	case "text", "raw":
		return decodeText, nil
	default:
		return nil, fmt.Errorf("%s: unknown stdout codec %q (want json|text|raw)", s.ID, s.Stdout)
	}
}

// FindCmd tries each entry in Find via lookPathCmd. The first candidate that
// resolves to a usable interpreter wins; on Windows, py.exe is probed with
// its selected version so an empty launcher is treated as missing.
//
// Each Find entry is split on whitespace; the first token is the binary,
// the rest are pre-args (e.g. "py -3" → cmd="py", args=["-3"]). The
// materialized source path is appended later by the runtime.
func (s RunnerSpec) FindCmd() (cmd string, args []string, err error) {
	return s.findCmd(lookPathCmd, func(cmd string, args []string) bool {
		if runtime.GOOS != "windows" || !isWindowsPythonLauncher(cmd) {
			return true
		}
		probeArgs := append(append([]string(nil), args...), "--version")
		return exec.CommandContext(context.Background(), cmd, probeArgs...).Run() == nil
	})
}

func (s RunnerSpec) findCmd(lookPath func(string) (string, error), usable func(string, []string) bool) (cmd string, args []string, err error) {
	for _, entry := range s.Find {
		parts := strings.Fields(entry)
		if len(parts) == 0 {
			continue
		}
		if foundCmd, lookupErr := lookPath(parts[0]); lookupErr == nil && usable(foundCmd, parts[1:]) {
			return foundCmd, parts[1:], nil
		}
	}
	return "", nil, ErrNotFound
}

func isWindowsPythonLauncher(cmd string) bool {
	cmd = strings.ToLower(strings.ReplaceAll(cmd, `\`, "/"))
	return path.Base(cmd) == "py.exe"
}

// --- embedded runners ----------------------------------------------------

// runnersFS holds the .nflow runner definition files shipped with the
// bundle. Loaded once at init() via LoadRunners.
//
//go:embed runners/*.nflow
var runnersFS embed.FS

// LoadRunners parses every runners/*.nflow file from the embed and registers
// each as a Language. Called once at init(); idempotent across reloads
// (duplicate registration panics — that's the project rule for builtins).
//
// One bad runner file does NOT kill the binary: registerSpec's panic is
// recovered and returned as an error, so the caller sees "file X failed
// because Y" rather than a process crash. This is the resilience rule —
// a malformed data file is user input, not a reason to panic the host.
func LoadRunners() error {
	entries, err := runnersFS.ReadDir("runners")
	if err != nil {
		return fmt.Errorf("lang: read runners/: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".nflow") {
			continue
		}
		raw, err := runnersFS.ReadFile(path.Join("runners", e.Name()))
		if err != nil {
			return fmt.Errorf("lang: read %s: %w", e.Name(), err)
		}
		spec, err := ParseRunner(string(raw))
		if err != nil {
			return fmt.Errorf("lang: parse %s: %w", e.Name(), err)
		}
		// registerSpec validates duplicate IDs and aliases before mutation.
		// Recover unexpected invariant panics so malformed embedded data
		// is reported as a runner-loading error rather than a raw crash.
		if err := safeRegisterSpec(spec); err != nil {
			return fmt.Errorf("lang: register %s: %w", e.Name(), err)
		}
	}
	return nil
}

// safeRegisterSpec wraps registerSpec with a recover, converting unexpected
// invariant panics to errors. Ordinary duplicate registrations are returned
// as validation errors by registerSpec.
func safeRegisterSpec(spec *RunnerSpec) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return registerSpec(spec)
}

// ParseRunner extracts the JSON object from a runner .nflow file and parses
// it into a RunnerSpec.
//
// The file format is: an optional `@lang.runner "id" ["version"]` header
// line, followed by a strict JSON object. The JSON object carries all
// fields including "id" and "version". The header is for human/IDE
// readability only — it's ignored by the parser, which trusts the JSON.
//
// This uses encoding/json directly (battle-tested by every Go program in
// existence). No hand-rolled brace counter, no comment stripper, no
// trailing-comma fixer. If a runner file needs documentation, it uses a
// "_comment" field inside the JSON — same convention as package.json.
//
// The parser finds the first '{' in the source and decodes from there
// via json.NewDecoder, which handles all JSON syntax correctly including
// nested objects and strings containing braces.
func ParseRunner(src string) (*RunnerSpec, error) {
	// Find the first '{' — the start of the JSON object. Everything
	// before it (the @lang.runner header, comments, blank lines) is
	// ignored. json.Decoder handles the matching '}' itself.
	brace := strings.IndexByte(src, '{')
	if brace < 0 {
		return nil, errors.New("no JSON object in runner file")
	}
	dec := json.NewDecoder(strings.NewReader(src[brace:]))
	var spec RunnerSpec
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("parse runner JSON: %w", err)
	}
	if spec.ID == "" {
		return nil, errors.New("runner: \"id\" is required")
	}
	if spec.Ext == "" {
		return nil, fmt.Errorf("@lang.runner %s: \"ext\" is required", spec.ID)
	}
	if len(spec.Find) == 0 {
		return nil, fmt.Errorf("@lang.runner %s: \"find\" is required", spec.ID)
	}
	if err := validateFileTemplate(&spec); err != nil {
		return nil, err
	}
	if spec.Deps != "" || spec.DepsMarker != "" {
		parts := strings.Fields(spec.Deps)
		if len(parts) < 2 || spec.DepsMarker == "" || !strings.Contains(strings.Join(parts[1:], " "), "{file}") {
			return nil, fmt.Errorf("@lang.runner %s: deps requires a command, deps_marker, and {file} argument", spec.ID)
		}
	}
	return &spec, nil
}

func validateFileTemplate(spec *RunnerSpec) error {
	if spec.FileTemplate == "" {
		return nil
	}
	if strings.TrimSpace(spec.FileTemplate) == "" {
		return fmt.Errorf("runner %s: \"file_template\" cannot be empty", spec.ID)
	}
	if (spec.Stdin != "" && spec.Stdin != "json") || (spec.Stdout != "" && spec.Stdout != "json") {
		return fmt.Errorf("runner %s: \"file_template\" requires stdin and stdout codecs to be json", spec.ID)
	}
	return nil
}

// registerSpec validates and converts a RunnerSpec to a Language, then
// registers it with its aliases. Validate every collision first so a
// user-supplied duplicate cannot panic or leave a partially registered spec.
func registerSpec(spec *RunnerSpec) error {
	if spec == nil || spec.ID == "" {
		return errors.New("lang: runner id is required")
	}
	if err := validateFileTemplate(spec); err != nil {
		return err
	}
	key := registryKey(spec.ID, spec.Version)
	if _, dup := Registry[key]; dup {
		return fmt.Errorf("lang: language %s already registered", key)
	}

	seen := make(map[string]struct{}, len(spec.Aliases))
	for _, alias := range spec.Aliases {
		if alias == "" {
			continue
		}
		if _, dup := seen[alias]; dup {
			return fmt.Errorf("lang: alias @%s is repeated in %s", alias, key)
		}
		seen[alias] = struct{}{}
		for _, registered := range aliases {
			if registered.Tag == alias {
				return fmt.Errorf("lang: alias @%s already registered for %s", alias, registryKey(registered.ID, registered.Version))
			}
		}
	}

	enc, err := spec.Encode()
	if err != nil {
		return err
	}
	dec, err := spec.Decode()
	if err != nil {
		return err
	}
	lang := Language{
		ID:      spec.ID,
		Ext:     spec.Ext,
		Encode:  enc,
		Decode:  dec,
		spec:    spec,
		Select:  specFunc(spec),
		install: spec.Install,
	}
	Register(lang)
	for _, alias := range spec.Aliases {
		registerAlias(alias, spec.ID, spec.Version)
	}
	return nil
}

// specFunc returns a Select function for a spec. Select is find-only:
// it never installs. Resolve() is the gate that calls Ensure on miss.
func specFunc(spec *RunnerSpec) func(string) (string, []string, error) {
	return func(_ string) (string, []string, error) {
		return spec.FindCmd()
	}
}
