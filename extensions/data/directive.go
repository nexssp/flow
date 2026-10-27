package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	compiler "github.com/nexssp/flow/compiler"
)

var DataDirective = compiler.Directive{
	Name:    "data",
	Example: `@data examples :from="examples.jsonl"`,
	Handler: handleData,
}

var DataUseDirective = compiler.Directive{
	Name:    "datause",
	Example: `@datause examples`,
	Handler: handleDataUse,
}

func handleData(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	tokens, err := splitDirectiveFields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req.Lines[req.I]), "@data")))
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data: %v", err)
	}
	if len(tokens) < 2 {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, `@data requires NAME :from="path.jsonl"`)
	}
	name := tokens[0]
	if !validDatasetName(name) {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data: invalid dataset name %q", name)
	}
	options := make(map[string]string, len(tokens)-1)
	for _, token := range tokens[1:] {
		key, rawValue, ok := strings.Cut(strings.TrimPrefix(token, ":"), "=")
		if !ok || key == "" {
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data: expected :from=\"path.jsonl\", got %q", token)
		}
		if key != "from" {
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data: unknown option %q (supported: :from)", key)
		}
		if _, duplicate := options[key]; duplicate {
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data: option :%s was specified more than once", key)
		}
		value, valueErr := parseDirectiveValue(rawValue)
		if valueErr != nil {
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data: %v", valueErr)
		}
		options[key] = value
	}
	from := strings.TrimSpace(options["from"])
	if from == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, `@data requires a non-empty :from="path.jsonl"`)
	}

	datasets, _ := req.Out[datasetMetaKey].(map[string]Dataset)
	if datasets == nil {
		datasets = make(map[string]Dataset)
		req.Out[datasetMetaKey] = datasets
	}
	if _, duplicate := datasets[name]; duplicate {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data: dataset %q is already declared", name)
	}
	path := from
	if !filepath.IsAbs(path) {
		path = filepath.Join(req.BaseDir, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data %s: resolve %q: %v", name, from, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data %s: stat %q: %v", name, path, err)
	}
	if !info.Mode().IsRegular() {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@data %s: source %q is not a regular file", name, path)
	}
	datasets[name] = Dataset{Name: name, Source: path}
	return compiler.DirectiveRes{Next: req.I + 1, BlankLines: []int{req.I}}, nil
}

func handleDataUse(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	tokens, err := splitDirectiveFields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req.Lines[req.I]), "@datause")))
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@datause: %v", err)
	}
	if len(tokens) != 1 {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@datause requires one declared dataset name; use data.split for train/validation/test partitions")
	}
	name := tokens[0]
	datasets, _ := req.Out[datasetMetaKey].(map[string]Dataset)
	if _, ok := datasets[name]; !ok {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@datause: dataset %q is not declared earlier with @data", name)
	}
	if !validDatasetName(name) {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@datause: invalid dataset name %q", name)
	}
	req.Body[req.I] = "data.rows:name=" + strconv.Quote(name)
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}

func validDatasetName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// splitDirectiveFields keeps whitespace inside quoted values intact.
func splitDirectiveFields(input string) ([]string, error) {
	var fields []string
	start := -1
	var quote rune
	escaped := false
	for index, current := range input {
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 {
			if current == '\\' {
				escaped = true
				continue
			}
			if current == quote {
				quote = 0
			}
			continue
		}
		if current == '"' || current == '\'' {
			if start < 0 {
				start = index
			}
			quote = current
			continue
		}
		if unicode.IsSpace(current) {
			if start >= 0 {
				fields = append(fields, input[start:index])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = index
		}
	}
	if escaped || quote != 0 {
		return nil, errors.New("unterminated quoted value")
	}
	if start >= 0 {
		fields = append(fields, input[start:])
	}
	return fields, nil
}

func parseDirectiveValue(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if value[0] == '"' {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid quoted value: %w", err)
		}
		return parsed, nil
	}
	if value[0] == '\'' {
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", errors.New("unterminated quoted value")
		}
		return value[1 : len(value)-1], nil
	}
	return value, nil
}
