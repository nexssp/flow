package selftest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/cli/selftest"
)

func TestAllNFlowConformance(t *testing.T) {
	sections := selftest.Sections()
	want := 0
	for _, section := range sections {
		ktest.RequireCondition(t, len(section.Features) > 0, "empty conformance section %q", section.Name)
		for _, feature := range section.Features {
			ktest.RequireCondition(t, feature.Skip == "", "fixture %q is skipped: %s", feature.Source, feature.Skip)
			ktest.RequireCondition(t, feature.DSL != "", "empty conformance feature %q", feature.Name)
			want++
		}
	}
	ktest.RequireCondition(t, want > 0, "no conformance features were discovered")

	var out bytes.Buffer
	code := selftest.Run(context.Background(), selftest.Options{
		Out:     &out,
		NoColor: true,
		JSON:    true,
	})
	ktest.RequireCondition(t, code == 0, "NFlow conformance self-test failed (exit %d):\n%s", code, out.String())
	var summary struct {
		Passed  int `json:"passed"`
		Failed  int `json:"failed"`
		Skipped int `json:"skipped"`
	}
	ktest.RequireNoError(t, json.Unmarshal(out.Bytes(), &summary))
	ktest.RequireEqual(t, summary.Passed, want)
	ktest.RequireEqual(t, summary.Failed, 0)
	ktest.RequireEqual(t, summary.Skipped, 0)
	t.Logf("NFlow conformance: %d passed, 0 failed, 0 skipped across %d sections", summary.Passed, len(sections))
}
