package flow_version

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

func runDirective(t *testing.T, line string) error {
	t.Helper()
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: []string{line},
		I:     0,
		Out:   map[string]any{},
		File:  "test.nflow",
	})
	return err
}

func withCompilerVersion(t *testing.T, version string) {
	t.Helper()
	original := compiler.Version
	compiler.Version = version
	t.Cleanup(func() { compiler.Version = original })
}

func TestNFlowVersion_Satisfied(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	for _, line := range []string{
		`@nflow_version "0.7.0"`,
		`@nflow_version "=0.7.0"`,
		`@nflow_version "==0.7.0"`,
		`@nflow_version ">=0.5.0"`,
		`@nflow_version ">=0.7.0"`,
		`@nflow_version "<=0.7.0"`,
		`@nflow_version "<=1.0.0"`,
		`@nflow_version ">0.5.0"`,
		`@nflow_version "<1.0.0"`,
		`@nflow_version "v0.7.0"`,
	} {
		t.Run(line, func(t *testing.T) {
			ktest.RequireNoError(t, runDirective(t, line))
		})
	}
}

func TestNFlowVersion_Unsatisfied(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	for _, line := range []string{
		`@nflow_version "0.8.0"`,
		`@nflow_version ">=0.8.0"`,
		`@nflow_version ">0.7.0"`,
		`@nflow_version "<=0.6.0"`,
		`@nflow_version "<0.7.0"`,
	} {
		t.Run(line, func(t *testing.T) {
			err := runDirective(t, line)
			ktest.RequireErrorContains(t, err, "not satisfied")
		})
	}
}

func TestNFlowVersion_RangeSatisfied(t *testing.T) {
	withCompilerVersion(t, "v0.38.0")

	for _, line := range []string{
		`@nflow_version ">=0.37.0 <0.40.0"`,
		`@nflow_version ">= 0.37.0 < 0.40.0"`,
		`@nflow_version ">=v0.37.0 <=v0.38.0 ==0.38.0"`,
		"@nflow_version \"  >=0.37.0\t<0.40.0  \"",
	} {
		t.Run(line, func(t *testing.T) {
			ktest.RequireNoError(t, runDirective(t, line))
		})
	}
}

func TestNFlowVersion_RangeUnsatisfied(t *testing.T) {
	withCompilerVersion(t, "v0.38.0")

	for _, line := range []string{
		`@nflow_version ">=0.37.0 <0.38.0"`,
		`@nflow_version ">0.38.0 <=0.40.0"`,
	} {
		t.Run(line, func(t *testing.T) {
			ktest.RequireErrorContains(t, runDirective(t, line), "not satisfied")
		})
	}
}

func TestNFlowVersion_RangeBoundaries(t *testing.T) {
	for _, version := range []string{"v0.37.0", "v0.39.99", "v0.36.99", "v0.40.0"} {
		t.Run(version, func(t *testing.T) {
			withCompilerVersion(t, version)
			err := runDirective(t, `@nflow_version ">=0.37.0 <0.40.0"`)
			if version == "v0.37.0" || version == "v0.39.99" {
				ktest.RequireNoError(t, err)
			} else {
				ktest.RequireErrorContains(t, err, "not satisfied")
			}
		})
	}
}

func TestNFlowVersion_DevCompilerSkipsCheck(t *testing.T) {
	withCompilerVersion(t, compiler.VersionUnknown)

	ktest.RequireNoError(t, runDirective(t, `@nflow_version ">=99.0.0"`))
	// Unknown/dev compilers preserve existing behavior: skip both parsing
	// and comparison because there is no release version to check against.
	ktest.RequireNoError(t, runDirective(t, `@nflow_version ">="`))
}

func TestNFlowVersion_MissingConstraintRejected(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	err := runDirective(t, `@nflow_version`)
	ktest.RequireErrorContains(t, err, "requires a constraint")
}

func TestNFlowVersion_MalformedConstraintRejected(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	for _, line := range []string{
		`@nflow_version ">="`,
		`@nflow_version ">=0.7.0 <"`,
		`@nflow_version ">=0.7.0 nonsense"`,
		`@nflow_version "   "`,
		`@nflow_version ">=0.7.0 || <0.8.0"`,
		`@nflow_version ">=0.7.0,<0.8.0"`,
		`@nflow_version "^0.7.0"`,
		`@nflow_version "~0.7.0"`,
		`@nflow_version "0.7.*"`,
	} {
		t.Run(line, func(t *testing.T) {
			ktest.RequireErrorContains(t, runDirective(t, line), "invalid constraint")
		})
	}
}

func TestNFlowVersion_DuplicateRejected(t *testing.T) {
	withCompilerVersion(t, "v0.7.0")

	req := compiler.DirectiveReq{
		Lines: []string{`@nflow_version ">=0.5.0"`},
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
		ktest.RequireStringContains(t, string(body), "@nflow_version")
	}
}
