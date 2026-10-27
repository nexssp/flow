package lang_test

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/nexssp/flow/cli/selftest"
	"github.com/nexssp/flow/extensions/lang"
)

func TestDefineFixtureExecutesWithAvailablePythonRunner(t *testing.T) {
	python, err := lang.Lookup("python", "")
	if err != nil {
		t.Fatalf("lookup built-in Python runner: %v", err)
	}
	_, _, pythonErr := python.Select("")

	var output bytes.Buffer
	code := selftest.Run(context.Background(), selftest.Options{
		Out:     &output,
		Filters: []string{"User-defined language via @lang.define."},
		JSON:    true,
	})
	if code != 0 {
		t.Fatalf("filtered self-test exit code = %d; output:\n%s", code, output.String())
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
		t.Fatalf("decode filtered self-test report: %v\n%s", err, output.String())
	}
	if report.Failed != 0 || report.Passed+report.Skipped != 1 {
		t.Fatalf("self-test results: passed=%d failed=%d skipped=%d; want exactly one pass or safe skip\n%s", report.Passed, report.Failed, report.Skipped, output.String())
	}
	for _, section := range report.Sections {
		for _, feature := range section.Features {
			if feature.Name != "User-defined language via @lang.define." {
				continue
			}
			if pythonErr == nil && feature.Status != "passed" {
				t.Fatalf("@lang.define fixture status = %q; want passed with built-in Python available; skip reason: %s", feature.Status, feature.SkipReason)
			}
			if pythonErr != nil && (feature.Status != "skipped" || feature.SkipReason == "") {
				t.Fatalf("@lang.define fixture status = %q, reason %q; want a safe skip without Python", feature.Status, feature.SkipReason)
			}
			if pythonErr == nil {
				defined, lookupErr := lang.Lookup("inlinepy", "")
				if lookupErr != nil {
					t.Fatalf("@lang.define did not register inlinepy: %v", lookupErr)
				}
				wantCmd, wantArgs, selectErr := python.Select("")
				if selectErr != nil {
					t.Fatalf("select built-in Python runner: %v", selectErr)
				}
				gotCmd, gotArgs, selectErr := defined.Select("")
				if selectErr != nil {
					t.Fatalf("select @lang.define runner: %v", selectErr)
				}
				if gotCmd != wantCmd || !slices.Equal(gotArgs, wantArgs) {
					t.Fatalf("@lang.define selected %q %#v; want built-in Python %q %#v", gotCmd, gotArgs, wantCmd, wantArgs)
				}
			}
			return
		}
	}
	t.Fatal("filtered self-test report did not include the @lang.define fixture")
}
