package selftest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/nexssp/flow/cli/selftest"
	"github.com/nexssp/flow/extensions/lang"
)

const rustFixtureRunnerEnv = "FLOW_SELFTEST_RUST_FIXTURE_RUNNER"

func TestRunRustFixtureWithAvailableRunner(t *testing.T) {
	const registryKey = "rust"
	original, ok := lang.Registry[registryKey]
	if !ok {
		t.Fatalf("language runner %q is not registered", registryKey)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	stubbedRunner := *original
	stubbedRunner.Select = func(string) (string, []string, error) {
		return executable, []string{"-test.run=^TestRustFixtureRunnerHelper$"}, nil
	}
	lang.Registry[registryKey] = &stubbedRunner
	t.Cleanup(func() { lang.Registry[registryKey] = original })
	t.Setenv(rustFixtureRunnerEnv, "1")

	var output bytes.Buffer
	code := selftest.Run(context.Background(), selftest.Options{
		Out:     &output,
		Filters: []string{"Inline rs tag executes a Rust script and returns text."},
		JSON:    true,
	})
	if code != 0 {
		t.Fatalf("selftest.Run exit code = %d; output:\n%s", code, output.String())
	}

	var report struct {
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
		Skipped  int `json:"skipped"`
		Sections []struct {
			Features []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"features"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode self-test report: %v\n%s", err, output.String())
	}
	if report.Passed != 1 || report.Failed != 0 || report.Skipped != 0 {
		t.Fatalf("self-test results: passed=%d failed=%d skipped=%d; want one pass\n%s", report.Passed, report.Failed, report.Skipped, output.String())
	}
	for _, section := range report.Sections {
		for _, feature := range section.Features {
			if feature.Name == "Inline rs tag executes a Rust script and returns text." {
				if feature.Status != "passed" {
					t.Fatalf("Rust fixture status = %q; want passed", feature.Status)
				}
				return
			}
		}
	}
	t.Fatal("self-test report did not include the Rust fixture")
}

// TestRustFixtureRunnerHelper is launched as the stubbed rust-script process.
func TestRustFixtureRunnerHelper(t *testing.T) {
	if os.Getenv(rustFixtureRunnerEnv) == "1" {
		if _, err := fmt.Fprintln(os.Stdout, `"ok"`); err != nil {
			t.Fatalf("write stubbed runner output: %v", err)
		}
	}
}
