package selftest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/runtime"
)

type fixtureWalkErrorFS struct {
	err error
}

func (f fixtureWalkErrorFS) Open(string) (fs.File, error) {
	return nil, f.err
}

func TestFixtureWalkFailureFailsSelfTest(t *testing.T) {
	for _, tc := range []struct {
		name string
		json bool
	}{
		{name: "text"},
		{name: "json", json: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantErr := errors.New("injected traversal failure")
			bundle := compiler.Bundle{
				ID:       "fixture-walk-error",
				Fixtures: fixtureWalkErrorFS{err: wantErr},
			}

			var output bytes.Buffer
			code := runWithBundles(context.Background(), Options{
				Out:     &output,
				Filters: []string{"fixture discovery failed"},
				JSON:    tc.json,
			}, []compiler.Bundle{runtime.Bundle(nil), bundle})
			if code != 1 {
				t.Fatalf("self-test exit code = %d, want 1; output:\n%s", code, output.String())
			}

			if !tc.json {
				if !strings.Contains(output.String(), "injected traversal failure") || !strings.Contains(output.String(), "FAILED") {
					t.Fatalf("text report did not show a failed discovery with its error:\n%s", output.String())
				}
				return
			}

			var report struct {
				Failed   int `json:"failed"`
				Skipped  int `json:"skipped"`
				Sections []struct {
					Features []struct {
						Name   string `json:"name"`
						Status string `json:"status"`
						Error  string `json:"error"`
					} `json:"features"`
				} `json:"sections"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatalf("decode self-test report: %v\n%s", err, output.String())
			}
			if report.Failed != 1 || report.Skipped != 0 || len(report.Sections) != 1 || len(report.Sections[0].Features) != 1 {
				t.Fatalf("JSON discovery report = %+v; want one failed and zero skipped feature\n%s", report, output.String())
			}
			feature := report.Sections[0].Features[0]
			if feature.Name != "fixture discovery failed" || feature.Status != "failed" || !strings.Contains(feature.Error, wantErr.Error()) {
				t.Fatalf("JSON discovery feature = %+v; want failed status and injected error", feature)
			}
		})
	}
}
