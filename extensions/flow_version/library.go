// Package flow_version provides the @flow_version directive, which
// rejects a .nflow file at compile time when the running compiler does
// not satisfy the declared version constraint. It gives flow authors a
// hard guarantee that a file will not be silently reinterpreted under
// different language semantics on a different release.
//
// Typical use:
//
//	@flow_version ">=0.7.0"
//
// A source build (core.Version == "dev") accepts every constraint —
// there is no release identity to compare against, and failing the
// build for a local `go run` would be counterproductive.
package flow_version

import (
	"cmp"
	"context"
	"embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "flow_version"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []core.Directive{Directive},
		Fixtures:   fixturesFS,
	}
}

// Directive parses `@flow_version "CONSTRAINT"` and rejects the source
// when the running compiler does not satisfy it. The constraint is one
// of `=`, `==`, `>=`, `<=`, `>`, `<`, or bare (implicit `=`), followed
// by a MAJOR.MINOR.PATCH version, optionally prefixed with `v` and
// optionally carrying a pre-release or build-metadata suffix that is
// ignored for comparison.
var Directive = core.Directive{
	Name:    "flow_version",
	Example: `@flow_version ">=0.7.0"`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}

	if _, exists := req.Out["flow_version"]; exists {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@flow_version: duplicate declaration")
	}

	raw := parseConstraintArg(req.Lines[req.I])
	if raw == "" {
		return core.DirectiveRes{}, core.SourceError(pos,
			`@flow_version requires a constraint, e.g. @flow_version ">=0.7.0"`)
	}
	req.Out["flow_version"] = raw

	if core.Version == "" || core.Version == core.VersionUnknown {
		return core.DirectiveRes{Next: req.I + 1}, nil
	}

	op, wantRaw := splitOperator(raw)
	want, err := parseSemver(wantRaw)
	if err != nil {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@flow_version: invalid constraint %q: %v", raw, err)
	}
	got, err := parseSemver(core.Version)
	if err != nil {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@flow_version: running compiler version %q is not parseable: %v",
			core.Version, err)
	}
	if !satisfies(op, got.compare(want)) {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@flow_version: constraint %q not satisfied by running compiler %q",
			raw, core.Version)
	}
	return core.DirectiveRes{Next: req.I + 1}, nil
}

// parseConstraintArg strips the directive name and any surrounding
// quotes, tolerating an optional `:` separator.
func parseConstraintArg(line string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "@flow_version"))
	rest = strings.TrimPrefix(rest, ":")
	return strings.Trim(strings.TrimSpace(rest), `"'`+"`")
}

// splitOperator separates the comparison operator from the version
// number. A constraint with no operator is treated as equality.
func splitOperator(constraint string) (op, version string) {
	for _, prefix := range []string{">=", "<=", "==", ">", "<", "="} {
		if after, ok := strings.CutPrefix(constraint, prefix); ok {
			return prefix, strings.TrimSpace(after)
		}
	}
	return "=", strings.TrimSpace(constraint)
}

// satisfies reports whether the running compiler, expressed as the
// sign of got.compare(want), meets the constraint operator.
func satisfies(op string, comparison int) bool {
	switch op {
	case "=", "==":
		return comparison == 0
	case ">=":
		return comparison >= 0
	case "<=":
		return comparison <= 0
	case ">":
		return comparison > 0
	case "<":
		return comparison < 0
	}
	return false
}

// semver is the minimum viable subset: three integer components, no
// pre-release or build-metadata comparison. Flow versions do not
// publish pre-releases today; if they ever do, this is the extension
// point.
type semver struct {
	major, minor, patch int
}

func (v semver) compare(other semver) int {
	if c := cmp.Compare(v.major, other.major); c != 0 {
		return c
	}
	if c := cmp.Compare(v.minor, other.minor); c != 0 {
		return c
	}
	return cmp.Compare(v.patch, other.patch)
}

func parseSemver(raw string) (semver, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if i := strings.IndexAny(trimmed, "-+"); i >= 0 {
		trimmed = trimmed[:i]
	}
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("expected MAJOR.MINOR.PATCH, got %q", raw)
	}
	var v semver
	var err error
	if v.major, err = strconv.Atoi(parts[0]); err != nil {
		return semver{}, fmt.Errorf("major: %w", err)
	}
	if v.minor, err = strconv.Atoi(parts[1]); err != nil {
		return semver{}, fmt.Errorf("minor: %w", err)
	}
	if v.patch, err = strconv.Atoi(parts[2]); err != nil {
		return semver{}, fmt.Errorf("patch: %w", err)
	}
	return v, nil
}
