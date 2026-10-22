package cli

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestCLIBundleConflictsRejectDuplicateIDs(t *testing.T) {
	for _, entry := range []string{"cli", "embedded", "factory", "embedded factory"} {
		t.Run(entry, func(t *testing.T) {
			var ran atomic.Int32
			var closed []string
			bundles := make([]compiler.Bundle, 0, 2)
			for _, value := range []string{"first", "second"} {
				bundles = append(bundles, lifecycleBundle("audit-dupe", func(context.Context) (any, error) {
					ran.Add(1)
					return value, nil
				}, func(context.Context) error {
					closed = append(closed, value)
					return nil
				}))
			}
			adoptAll := func(adopt func(compiler.Bundle) error) error {
				for _, bundle := range bundles {
					if err := adopt(bundle); err != nil {
						return err
					}
				}
				return nil
			}
			path := writeLifecycleFlow(t, "audit-dupe.run\n")
			var code int
			stdout, stderr := captureIO(t, func() {
				switch entry {
				case "cli":
					code = RunWithBundles([]string{"run", path, "--json"}, bundles)
				case "embedded":
					code = RunEmbeddedWithBundles(t.Context(), "audit-dupe.run", []string{"--json"}, bundles)
				case "factory":
					code = RunWithBundleFactoriesForRequirements([]string{"run", path, "--json"}, nil, adoptAll)
				case "embedded factory":
					code = RunEmbeddedWithBundleFactoriesForRequirements(t.Context(), "audit-dupe.run", []string{"--json"}, nil, adoptAll)
				}
			})
			if code == 0 || !strings.Contains(stderr, `duplicate bundle "audit-dupe"`) {
				t.Fatalf("exit=%d stderr=%q; want a named duplicate bundle error", code, stderr)
			}
			if stdout != "" || ran.Load() != 0 {
				t.Fatalf("stdout=%q action calls=%d; no conflicting action may run", stdout, ran.Load())
			}
			if got := strings.Join(closed, ","); got != "second,first" {
				t.Fatalf("cleanup=%q, want second,first", got)
			}
		})
	}
}

func TestCLIBundleConflictsRejectNativeAndIdenticalOverlaps(t *testing.T) {
	for _, test := range []struct {
		name    string
		bundles []compiler.Bundle
		source  string
		wantID  string
	}{
		{name: "injected native", bundles: []compiler.Bundle{{ID: "runtime"}}, source: "runtime.noop", wantID: "runtime"},
		{name: "injected retry", bundles: []compiler.Bundle{{ID: "retry"}}, source: "runtime.noop", wantID: "retry"},
		{name: "identical empty bundles", bundles: []compiler.Bundle{{ID: "same"}, {ID: "same"}}, source: "runtime.noop", wantID: "same"},
		{name: "nonadjacent duplicates", bundles: []compiler.Bundle{{ID: "same"}, {ID: "other"}, {ID: "same"}}, source: "runtime.noop", wantID: "same"},
		{name: "required native", source: "@require runtime\nruntime.noop", wantID: "runtime"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var code int
			stdout, stderr := captureIO(t, func() {
				code = RunEmbeddedWithBundles(t.Context(), test.source, []string{"--json"}, test.bundles)
			})
			if code == 0 || !strings.Contains(stderr, `duplicate bundle "`+test.wantID+`"`) || stdout != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q; want duplicate %q with no output", code, stdout, stderr, test.wantID)
			}
		})
	}
}

func TestCLIBundleConflictsPreserveConfiguredRetry(t *testing.T) {
	var attempts atomic.Int32
	bundle := lifecycleBundle("retry_probe", func(context.Context) (any, error) {
		attempts.Add(1)
		return nil, errors.New("permanent probe failure")
	}, nil)
	source := `@require retry { default_max: "2", backoff: "constant", min_backoff: "1ns", max_backoff: "1ns", retry_all: "true" }
retry_probe.run:retry`
	var code int
	_, stderr := captureIO(t, func() {
		code = RunEmbeddedWithBundles(t.Context(), source, []string{"--json"}, []compiler.Bundle{bundle})
	})
	if code == 0 || strings.Contains(stderr, "duplicate bundle") || attempts.Load() != 3 {
		t.Fatalf("exit=%d attempts=%d stderr=%q; want configured retry's three failed attempts", code, attempts.Load(), stderr)
	}
}

func TestCLIBundleConflictsPreserveResilienceSelection(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		wantOK bool
	}{
		{name: "resilience", source: "@require resilience\nruntime.noop", wantOK: true},
		{name: "conflicting policies", source: "@require resilience\n@require retry\nruntime.noop"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var code int
			_, stderr := captureIO(t, func() {
				code = RunEmbeddedWithBundles(t.Context(), test.source, []string{"--json"}, nil)
			})
			if test.wantOK {
				if code != 0 {
					t.Fatalf("exit=%d stderr=%q; want success", code, stderr)
				}
			} else if code == 0 || !strings.Contains(stderr, "cannot be used together") {
				t.Fatalf("exit=%d stderr=%q; want retry/resilience policy conflict", code, stderr)
			}
		})
	}
}
