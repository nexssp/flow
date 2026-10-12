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

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/schema"
)

var ConstDirective = compiler.Directive{
	Name:    "const",
	Example: `@const DLQ_SUBJECT = "orders.dlq"`,
	Handler: handleConst,
}

var ConstLoadDirective = compiler.Directive{
	Name:    "const.load",
	Example: `@const.load "./config.json" as env :schema=ConfigType`,
	Handler: handleConstLoad,
}

func handleConst(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	line := strings.TrimSpace(strings.TrimPrefix(req.Lines[req.I], "@const"))
	key, val, ok := strings.Cut(line, "=")
	if !ok {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@const requires KEY = VALUE format",
		)
	}

	cleanKey := strings.TrimSpace(key)
	cleanVal := compiler.TrimQuotes(strings.TrimSpace(val))

	storeAndApply(req, map[string]string{cleanKey: cleanVal})
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}

func handleConstLoad(ctx context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(strings.TrimPrefix(req.Lines[req.I], "@const.load"))

	path, namespace, schemaName, err := parseConstLoadLine(line, req.BaseDir)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@const.load: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@const.load read error: %v", err)
	}

	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@const.load json decode error: %v", err)
	}

	if schemaName != "" {
		if err := validateConstLoadSchema(ctx, req.Out, schemaName, parsed, pos); err != nil {
			return compiler.DirectiveRes{}, err
		}
	}

	flat := make(map[string]string)
	flattenJSON(namespace, parsed, flat)

	storeAndApply(req, flat)
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}

func parseConstLoadLine(line, baseDir string) (path, namespace, schemaName string, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[1] != "as" {
		return "", "", "", errors.New(`requires "path/to/file.json" as namespace [:schema=Name] format`)
	}

	path = compiler.TrimQuotes(fields[0])
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

func validateConstLoadSchema(ctx context.Context, out map[string]any, name string, parsed any, pos compiler.Position) error {
	declared := schema.SchemasFromMap(out)
	if len(declared) == 0 {
		return compiler.SourceError(pos,
			"@const.load :schema=%s: no @schema declared in this source", name)
	}
	decl, ok := schema.ByName(out, name)
	if !ok {
		return compiler.SourceError(pos,
			"@const.load :schema=%s: unknown schema (declared: %s)",
			name, declaredSchemaNames(declared))
	}
	if err := schema.Validate(ctx, decl, parsed); err != nil {
		return compiler.SourceError(pos, "@const.load :schema=%s: %s",
			name, describeValidationError(err))
	}
	return nil
}

func describeValidationError(err error) string {
	appErr, ok := errors.AsType[*xerr.AppError](err)
	if !ok || len(appErr.ValidationDetails) == 0 {
		return err.Error()
	}
	parts := make([]string, 0, len(appErr.ValidationDetails))
	for _, d := range appErr.ValidationDetails {
		if d.Value != "" {
			parts = append(parts, d.Field+" "+d.Value)
			continue
		}
		parts = append(parts, d.Field+" failed "+d.Validation+" check")
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

func storeAndApply(req compiler.DirectiveReq, newConsts map[string]string) {
	existing, _ := req.Out["constants"].(map[string]string)
	if existing == nil {
		existing = make(map[string]string, len(newConsts))
		req.Out["constants"] = existing
	}
	maps.Copy(existing, newConsts)

	Apply(req.Lines, req.I+1, newConsts)
}

func Apply(lines []string, start int, constants map[string]string) {
	compiler.ApplyConstants(lines, start, constants)
}
