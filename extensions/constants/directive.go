package constants

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/schema"
)

var ConstDirective = core.Directive{
	Name:    "const",
	Example: `@const DLQ_SUBJECT = "orders.dlq"`,
	Handler: handleConst,
}

var ConstLoadDirective = core.Directive{
	Name:    "const.load",
	Example: `@const.load "./config.json" as env :schema=ConfigType`,
	Handler: handleConstLoad,
}

func handleConst(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	line := strings.TrimSpace(strings.TrimPrefix(req.Lines[req.I], "@const"))
	key, val, ok := strings.Cut(line, "=")
	if !ok {
		return core.DirectiveRes{}, core.SourceError(
			core.Position{File: req.File, Line: req.I + 1},
			"@const requires KEY = VALUE format",
		)
	}

	cleanKey := strings.TrimSpace(key)
	cleanVal := core.TrimQuotes(strings.TrimSpace(val))

	storeAndApply(req, map[string]string{cleanKey: cleanVal})
	return core.DirectiveRes{Next: req.I + 1}, nil
}

func handleConstLoad(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(strings.TrimPrefix(req.Lines[req.I], "@const.load"))

	path, namespace, schemaName, err := parseConstLoadLine(line, req.BaseDir)
	if err != nil {
		return core.DirectiveRes{}, core.SourceError(pos, "@const.load: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@const.load read error: %v", err)
	}

	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@const.load json decode error: %v", err)
	}

	if schemaName != "" {
		if err := validateConstLoadSchema(req.Out, schemaName, parsed, pos); err != nil {
			return core.DirectiveRes{}, err
		}
	}

	flat := make(map[string]string)
	flattenJSON(namespace, parsed, flat)

	storeAndApply(req, flat)
	return core.DirectiveRes{Next: req.I + 1}, nil
}

// parseConstLoadLine parses: "<path>" as <namespace> [:schema=<Name>]
// The path may be quoted with any of " ' `; the namespace is a bare
// identifier; the :schema modifier is optional.
func parseConstLoadLine(line, baseDir string) (path, namespace, schemaName string, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[1] != "as" {
		return "", "", "", errors.New(`requires "path/to/file.json" as namespace [:schema=Name] format`)
	}

	path = core.TrimQuotes(fields[0])
	namespace = fields[2]

	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}

	for _, token := range fields[3:] {
		name, value, ok := strings.Cut(token, "=")
		if !ok {
			continue
		}
		if name == ":schema" && value != "" {
			schemaName = value
		}
	}
	return path, namespace, schemaName, nil
}

// validateConstLoadSchema fails the build when the loaded JSON does not
// match the declared @schema. Ordering is deliberate: the schema must
// appear above the @const.load in the same file, matching the
// declaration-before-use rule that @profile and @pipeline already obey.
func validateConstLoadSchema(out map[string]any, name string, parsed any, pos core.Position) error {
	declared := schema.SchemasFromMap(out)
	if len(declared) == 0 {
		return core.SourceError(pos,
			"@const.load :schema=%s: no @schema declared in this source", name)
	}
	decl, ok := schema.ByName(out, name)
	if !ok {
		return core.SourceError(pos,
			"@const.load :schema=%s: unknown schema (declared: %s)",
			name, declaredSchemaNames(declared))
	}
	if err := schema.Validate(decl, parsed); err != nil {
		return core.SourceError(pos, "@const.load :schema=%s: %s",
			name, describeValidationError(err))
	}
	return nil
}

// describeValidationError renders an xerr.AppError with its per-field
// details. schema.Validate returns an *xerr.AppError whose Error() is
// only "schema X: validation failed"; the actionable information lives
// in ValidationDetails, one entry per failed field. Without this the
// compile-time diagnostic would name the schema but not the field the
// user mistyped.
func describeValidationError(err error) string {
	appErr, ok := errors.AsType[*xerr.AppError](err)
	if !ok || len(appErr.ValidationDetails) == 0 {
		return err.Error()
	}
	parts := make([]string, 0, len(appErr.ValidationDetails))
	for _, d := range appErr.ValidationDetails {
		field := d.Field + ": " + d.Validation
		if d.Value != "" {
			field += " (" + d.Value + ")"
		}
		parts = append(parts, field)
	}
	return appErr.Message + ": " + strings.Join(parts, ", ")
}

func declaredSchemaNames(declared []schema.Schema) string {
	names := make([]string, 0, len(declared))
	for _, s := range declared {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

func storeAndApply(req core.DirectiveReq, newConsts map[string]string) {
	existing, _ := req.Out["constants"].(map[string]string)
	if existing == nil {
		existing = make(map[string]string, len(newConsts))
		req.Out["constants"] = existing
	}
	maps.Copy(existing, newConsts)

	Apply(req.Lines, req.I+1, newConsts)
}

// Apply rewrites `${key}` references in lines[start:] to their values.
// It delegates to core.ApplyConstants, which is also what the @include
// merge path calls — one substitution primitive, one behavior.
func Apply(lines []string, start int, constants map[string]string) {
	core.ApplyConstants(lines, start, constants)
}
