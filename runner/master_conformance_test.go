package runner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestNativeMasterSupportedFixture(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	source, err := os.ReadFile(filepath.Join("testdata", "master_supported.nflow"))
	ktest.RequireNoError(t, err)
	execution, err := runner.Execute(t.Context(), cfg, string(source), "master_supported.nflow", nil)
	ktest.RequireNoError(t, err)
	asserts, ok := execution.Meta["asserts"].([]string)
	ktest.RequireCondition(t, ok && len(asserts) == 10, "want ten fixture-declared post-run assertions, got %#v", execution.Meta["asserts"])
	if failures := runner.EvaluateAssertions(execution.Output, asserts); len(failures) != 0 {
		t.Fatalf("post-run assertions failed (output %#v): %v", execution.Output, failures)
	}
}

func TestMasterResilienceWithExplicitHostStore(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	store, err := runner.NewFileJournal(t.TempDir())
	ktest.RequireNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	cfg.IdempotencyStore = store
	source, err := os.ReadFile(filepath.Join("testdata", "master_proposed.nflow"))
	ktest.RequireNoError(t, err)
	prepared, err := runner.Preflight(t.Context(), cfg, string(source), "master_proposed.nflow")
	ktest.RequireNoError(t, err)
	ctx := xctx.WithRequestID(t.Context(), "Maks-Julia-ingest")
	execution, err := runner.ExecutePrepared(ctx, cfg, prepared, runner.EntryPoint{
		Name: "pipeline.resilient_ingest", Payload: map[string]any{},
	})
	ktest.RequireNoError(t, err)
	if failures := runner.EvaluateAssertions(execution.Output, []string{
		`result.ingest_status == "SUCCESS"`, `result.storage_code == 201`,
	}); len(failures) != 0 {
		t.Fatalf("explicit-host ingest assertions: %v", failures)
	}
}

func TestOriginalMasterDeclarationPrefixCompiles(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	source, err := os.ReadFile(filepath.Join("testdata", "master_proposed.nflow"))
	ktest.RequireNoError(t, err)
	marker := "# ── 8."
	prefix, _, ok := strings.Cut(string(source), marker)
	ktest.RequireCondition(t, ok, "master source missing assembly boundary %q", marker)
	_, err = runner.Compile(t.Context(), cfg, prefix, "master_proposed-declarations.nflow")
	ktest.RequireNoError(t, err)
}

func TestOriginalMasterExecutesAllAssertions(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	source, err := os.ReadFile(filepath.Join("testdata", "master_proposed.nflow"))
	ktest.RequireNoError(t, err)
	execution, err := runner.Execute(t.Context(), cfg, string(source), "master_proposed.nflow", nil)
	ktest.RequireNoError(t, err)
	asserts, ok := execution.Meta["asserts"].([]string)
	ktest.RequireCondition(t, ok && len(asserts) == 11, "want all eleven original master assertions, got %#v", execution.Meta["asserts"])
	if failures := runner.EvaluateAssertions(execution.Output, asserts); len(failures) != 0 {
		t.Fatalf("original master assertions failed (output %#v): %v", execution.Output, failures)
	}
}

func TestMasterModifierChainCompilesWithNativeBundles(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	source := `runtime.const :rate_limit=50 :idempotent :dedup :coalesce :tag="resilience,core" :desc="Hardened ingest endpoint" @{ value: "ok" }`
	_, err = runner.Compile(t.Context(), cfg, source, "modifier-chain.nflow")
	ktest.RequireNoError(t, err)
}

func TestMasterNestedProjectionCompositionExecutes(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	source := `{ peak: 98, keep: "same" } -> { snapshot: ., branches: parallel { first: { peak: .peak, keep: .keep }, second: race { runtime.fail @{ kind: "Unavailable", message: "first fails" }, runtime.const @{ value: { peak: .peak, keep: .keep } } } }, category: match(.peak) { > 90 -> "HIGH", _ -> "NORMAL" } }`
	execution, err := runner.Execute(t.Context(), cfg, source, "composition.nflow", nil)
	ktest.RequireNoError(t, err)
	if failures := runner.EvaluateAssertions(execution.Output, []string{
		`result.snapshot.peak == 98`, `result.branches.first.keep == "same"`,
		`result.branches.second.peak == 98`, `result.category == "HIGH"`,
	}); len(failures) != 0 {
		t.Fatalf("composition assertions failed (output %#v): %v", execution.Output, failures)
	}
}

func TestCompositionErrorReachesOuterTypedRecovery(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	source := `{ result: parallel { failed: runtime.fail @{ kind: "Unavailable", message: "nested source failed" } } } ? on_error { xerr.KindUnavailable -> { recovered: true }, else -> error.reraise }`
	execution, err := runner.Execute(t.Context(), cfg, source, "composition-recovery.nflow", nil)
	ktest.RequireNoError(t, err)
	if failures := runner.EvaluateAssertions(execution.Output, []string{`result.recovered == true`}); len(failures) != 0 {
		t.Fatalf("outer typed recovery assertions failed (output %#v): %v", execution.Output, failures)
	}
}
