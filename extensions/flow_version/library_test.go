package flow_version

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func runDirective(t *testing.T, line string) error {
	t.Helper()
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: []string{line},
		I:     0,
		Out:   map[string]any{},
		File:  "test.nflow",
	})
	return err
}

func withCompilerVersion(t *testing.T, version string) {
	t.Helper()
	original := core.Version
	core.Version = version
	t.Cleanup(func() { core.Version = original })
}

func TestFlowVersion_Satisfied(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	for _, line := range []string{
		`@flow_version "0.7.0"`,
		`@flow_version "=0.7.0"`,
		`@flow_version "==0.7.0"`,
		`@flow_version ">=0.5.0"`,
		`@flow_version ">=0.7.0"`,
		`@flow_version "<=0.7.0"`,
		`@flow_version "<=1.0.0"`,
		`@flow_version ">0.5.0"`,
		`@flow_version "<1.0.0"`,
		`@flow_version "v0.7.0"`,
	} {
		t.Run(line, func(t *testing.T) {
			ktest.RequireNoError(t, runDirective(t, line))
		})
	}
}

func TestFlowVersion_Unsatisfied(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	for _, line := range []string{
		`@flow_version "0.8.0"`,
		`@flow_version ">=0.8.0"`,
		`@flow_version ">0.7.0"`,
		`@flow_version "<=0.6.0"`,
		`@flow_version "<0.7.0"`,
	} {
		t.Run(line, func(t *testing.T) {
			err := runDirective(t, line)
			ktest.RequireErrorContains(t, err, "not satisfied")
		})
	}
}

func TestFlowVersion_DevCompilerSkipsCheck(t *testing.T) {
	withCompilerVersion(t, core.VersionUnknown)

	ktest.RequireNoError(t, runDirective(t, `@flow_version ">=99.0.0"`))
}

func TestFlowVersion_MissingConstraintRejected(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	err := runDirective(t, `@flow_version`)
	ktest.RequireErrorContains(t, err, "requires a constraint")
}

func TestFlowVersion_MalformedConstraintRejected(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	err := runDirective(t, `@flow_version ">="`)
	ktest.RequireErrorContains(t, err, "invalid constraint")
}

func TestFlowVersion_DuplicateRejected(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	req := core.DirectiveReq{
		Lines: []string{`@flow_version ">=0.5.0"`},
		I:     0,
		Out:   map[string]any{},
		File:  "test.nflow",
	}
	_, err := handleDirective(context.Background(), req)
	ktest.RequireNoError(t, err)

	_, err = handleDirective(context.Background(), req)
	ktest.RequireErrorContains(t, err, "duplicate declaration")
}

func TestParseSemver(t *testing.T) {
	for _, c := range []struct {
		raw               string
		major, minor, pat int
	}{
		{"1.2.3", 1, 2, 3},
		{"v1.2.3", 1, 2, 3},
		{"1.2.3-alpha", 1, 2, 3},
		{"1.2.3+build", 1, 2, 3},
		{"1.2.3-alpha+build", 1, 2, 3},
		{"0.0.0", 0, 0, 0},
	} {
		t.Run(c.raw, func(t *testing.T) {
			v, err := parseSemver(c.raw)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, v.major, c.major)
			ktest.RequireEqual(t, v.minor, c.minor)
			ktest.RequireEqual(t, v.patch, c.pat)
		})
	}
}

func TestParseSemver_Errors(t *testing.T) {
	for _, raw := range []string{"", "1.2", "1.2.3.4", "abc", "1.x.3"} {
		t.Run(raw, func(t *testing.T) {
			_, err := parseSemver(raw)
			ktest.RequireNotNil(t, err)
		})
	}
}

func TestFixtures_EmbeddedAndDescribed(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)
	ktest.RequireNotNil(t, bundle.Fixtures)

	entries, err := fs.ReadDir(bundle.Fixtures, "nflows")
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, len(entries) >= 2,
		"expected at least 2 fixtures, got %d", len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nflow") {
			continue
		}
		body, err := fs.ReadFile(bundle.Fixtures, "nflows/"+entry.Name())
		ktest.RequireNoError(t, err)
		ktest.RequireStringContains(t, string(body), "@description")
		ktest.RequireStringContains(t, string(body), "@flow_version")
	}
}
