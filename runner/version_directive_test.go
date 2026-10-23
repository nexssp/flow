package runner_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestNFlowVersionDirectiveSyntax(t *testing.T) {
	original := compiler.Version
	t.Cleanup(func() { compiler.Version = original })
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	for _, version := range []string{"v0.38.0", compiler.VersionUnknown} {
		t.Run(version, func(t *testing.T) {
			compiler.Version = version
			for _, tc := range []struct {
				name   string
				source string
				wantOK bool
			}{
				{"new", `@nflow_version ">=0.38.0"`, true},
				{"old", `@flow_version ">=0.38.0"`, false},
				{"duplicate", "@nflow_version \">=0.38.0\"\n@nflow_version \"<1.0.0\"", false},
				{"old_then_new", "@flow_version \">=0.38.0\"\n@nflow_version \">=0.38.0\"", false},
				{"new_then_old", "@nflow_version \">=0.38.0\"\n@flow_version \">=0.38.0\"", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					_, err := runner.Compile(t.Context(), cfg, tc.source+"\nconst @{ value: \"ok\" }\n", "version.nflow")
					if tc.wantOK {
						ktest.RequireNoError(t, err)
					} else {
						ktest.RequireNotNil(t, err)
					}
				})
			}
		})
	}
}
