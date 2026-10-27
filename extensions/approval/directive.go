package approval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexssp/flow/compiler"
)

const metadataKey = "approvals"

// Declaration describes one `@approval NAME { ... }` gate.
type Declaration struct {
	Name     string
	Role     string
	Reason   string
	Expires  time.Duration
	Position compiler.Position
}

// Directive parses an approval declaration and stores it in compiler metadata.
var Directive = compiler.Directive{
	Name:        "approval",
	Example:     "@approval finance_review {\n  role: reviewer\n  reason: confirm the transfer\n  expires: 30m\n}",
	Handler:     handleDirective,
	OpaqueBlock: true,
}

type approvalHeader struct {
	name     string
	rest     string
	open     int
	position compiler.Position
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	header, err := parseApprovalHeader(req)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}
	decl, next, err := parseApprovalBody(req, header)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}
	if decl.Role == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(header.position, "@approval %s: `role` is required", header.name)
	}
	if decl.Reason == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(header.position, "@approval %s: `reason` is required", header.name)
	}

	declarations := DeclarationsFromMeta(req.Out)
	if err := req.Declare("approval", header.name); err != nil {
		return compiler.DirectiveRes{}, err
	}
	for _, existing := range declarations {
		if existing.Name == header.name {
			return compiler.DirectiveRes{}, compiler.SourceError(header.position, "@approval %s: duplicate declaration (already at %s)", header.name, existing.Position)
		}
	}

	req.Out[metadataKey] = append(declarations, decl)
	return compiler.DirectiveRes{Next: next}, nil
}

func parseApprovalHeader(req compiler.DirectiveReq) (approvalHeader, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	position := compiler.Position{File: req.File, Line: req.I + 1}
	rest, ok := strings.CutPrefix(line, "@approval")
	if !ok {
		return approvalHeader{}, compiler.SourceError(position, "@approval: expected `@approval NAME { ... }`")
	}
	rest = strings.TrimSpace(rest)
	open := strings.IndexByte(rest, '{')
	if open < 0 {
		return approvalHeader{}, compiler.SourceError(position, "@approval: expected `@approval NAME { ... }`")
	}
	name := strings.TrimSpace(rest[:open])
	if err := validateName(name); err != nil {
		return approvalHeader{}, compiler.SourceError(position, "@approval: %v", err)
	}
	return approvalHeader{name: name, rest: rest, open: open, position: position}, nil
}

func parseApprovalBody(req compiler.DirectiveReq, header approvalHeader) (Declaration, int, error) {
	decl := Declaration{Name: header.name, Expires: 30 * time.Minute, Position: header.position}
	seen := make(map[string]bool, 3)
	addLine := func(raw string, lineNo int) error {
		return addApprovalField(req.File, header.name, &decl, seen, raw, lineNo)
	}

	// A field may follow the opening brace on the declaration line.
	remainder := header.rest[header.open+1:]
	closeAt := strings.IndexByte(remainder, '}')
	next := req.I + 1
	if closeAt >= 0 {
		if hasUnexpectedTrailingText(remainder[closeAt+1:]) {
			return Declaration{}, 0, compiler.SourceError(header.position, "@approval %s: unexpected text after closing brace", header.name)
		}
		if err := addLine(remainder[:closeAt], req.I+1); err != nil {
			return Declaration{}, 0, err
		}
		return decl, next, nil
	}
	if err := addLine(remainder, req.I+1); err != nil {
		return Declaration{}, 0, err
	}

	closed := false
	for i := req.I + 1; i < len(req.Lines); i++ {
		current := req.Lines[i]
		closeAt = strings.IndexByte(current, '}')
		if closeAt >= 0 {
			if hasUnexpectedTrailingText(current[closeAt+1:]) {
				position := compiler.Position{File: req.File, Line: i + 1}
				return Declaration{}, 0, compiler.SourceError(position, "@approval %s: unexpected text after closing brace", header.name)
			}
			if err := addLine(current[:closeAt], i+1); err != nil {
				return Declaration{}, 0, err
			}
			next = i + 1
			closed = true
			break
		}
		if err := addLine(current, i+1); err != nil {
			return Declaration{}, 0, err
		}
	}
	if !closed {
		return Declaration{}, 0, compiler.SourceError(header.position, "@approval %s: missing closing brace", header.name)
	}
	return decl, next, nil
}

func addApprovalField(file, name string, decl *Declaration, seen map[string]bool, raw string, lineNo int) error {
	field := strings.TrimSpace(raw)
	if field == "" || strings.HasPrefix(field, "#") || strings.HasPrefix(field, "//") {
		return nil
	}
	position := compiler.Position{File: file, Line: lineNo}
	key, value, found := strings.Cut(field, ":")
	if !found {
		return compiler.SourceError(position, "@approval %s: expected `key: value`, got %q", name, field)
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), ","))
	if seen[key] {
		return compiler.SourceError(position, "@approval %s: duplicate field %q", name, key)
	}
	seen[key] = true
	value = unquote(value)
	if value == "" {
		return compiler.SourceError(position, "@approval %s: %s cannot be empty", name, key)
	}
	switch key {
	case "role":
		decl.Role = value
	case "reason":
		decl.Reason = value
	case "expires":
		duration, err := time.ParseDuration(value)
		if err != nil {
			return compiler.SourceError(position, "@approval %s: expires: %v", name, err)
		}
		if duration <= 0 {
			return compiler.SourceError(position, "@approval %s: expires must be greater than zero", name)
		}
		decl.Expires = duration
	default:
		return compiler.SourceError(position, "@approval %s: unknown field %q (role|reason|expires)", name, key)
	}
	return nil
}

func hasUnexpectedTrailingText(value string) bool {
	trailing := strings.TrimSpace(value)
	return trailing != "" && !strings.HasPrefix(trailing, "#") && !strings.HasPrefix(trailing, "//")
}

// DeclarationsFromMeta returns a copy of the approval declarations in metadata.
func DeclarationsFromMeta(meta map[string]any) []Declaration {
	if meta == nil {
		return nil
	}
	declarations, _ := meta[metadataKey].([]Declaration)
	return append([]Declaration(nil), declarations...)
}

func validateName(name string) error {
	if name == "" {
		return errors.New("missing name — expected `@approval NAME { ... }`")
	}
	for i := range len(name) {
		c := name[i]
		valid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9' && i > 0) || c == '_' || c == '-' || c == '.'
		if !valid {
			return fmt.Errorf("invalid character %q in approval name", string(c))
		}
	}
	return nil
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	return value
}
