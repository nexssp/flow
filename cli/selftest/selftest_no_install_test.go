package selftest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nexssp/flow/cli/selftest"
	"github.com/nexssp/flow/extensions/lang"
)

func TestRunSkipsMissingLanguageRuntimeWithoutInstalling(t *testing.T) {
	const registryKey = "python@3"
	original, ok := lang.Registry[registryKey]
	if !ok {
		t.Fatalf("language runner %q is not registered", registryKey)
	}
	forcedMiss := *original
	forcedMiss.Select = func(version string) (string, []string, error) {
		if version != "3" {
			t.Errorf("python Select version = %q; want 3", version)
		}
		return "", nil, lang.ErrNotFound
	}
	lang.Registry[registryKey] = &forcedMiss
	t.Cleanup(func() { lang.Registry[registryKey] = original })

	// Keep package managers and language runtimes out of PATH. If self-test
	// accidentally attempts installation, its skip reason reports the failed
	// install recipe instead of the clean missing-runtime result asserted below.
	t.Setenv("PATH", t.TempDir())

	var output bytes.Buffer
	code := selftest.Run(context.Background(), selftest.Options{
		Out:     &output,
		Filters: []string{"Version-pinned aliases"},
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
				Name       string `json:"name"`
				Status     string `json:"status"`
				SkipReason string `json:"skip_reason"`
			} `json:"features"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode self-test report: %v\n%s", err, output.String())
	}
	if report.Failed != 0 || report.Skipped != 1 {
		t.Fatalf("self-test results: passed=%d failed=%d skipped=%d; want one skip and no failures\n%s", report.Passed, report.Failed, report.Skipped, output.String())
	}
	var found bool
	for _, section := range report.Sections {
		for _, feature := range section.Features {
			if !strings.Contains(feature.Name, "Version-pinned aliases") {
				continue
			}
			found = true
			if feature.Status != "skipped" || !strings.Contains(feature.SkipReason, "python@3") {
				t.Errorf("feature result = status %q, skip reason %q; want missing python@3 skip", feature.Status, feature.SkipReason)
			}
			if strings.Contains(feature.SkipReason, "install recipe") {
				t.Errorf("self-test attempted installation: skip reason %q", feature.SkipReason)
			}
		}
	}
	if !found {
		t.Fatal("self-test report did not include the filtered Python 3 fixture")
	}
}
