package runtime

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

func run(tb testing.TB, act action.AnyAction, in any) (any, error) {
	tb.Helper()
	return action.InvokeAny(tb.Context(), act, in)
}

// ── const ────────────────────────────────────────────────────────

func TestConst_Coercion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"bare string", "hello", "hello"},
		{"int", map[string]any{"value": "42"}, int64(42)},
		{"numeric int", map[string]any{"value": float64(42)}, int64(42)},
		{"negative int", map[string]any{"value": "-7"}, int64(-7)},
		{"float", map[string]any{"value": "3.14"}, 3.14},
		{"numeric float", map[string]any{"value": float64(3.14)}, 3.14},
		{"true", map[string]any{"value": "true"}, true},
		{"false", map[string]any{"value": "false"}, false},
		{"null", map[string]any{"value": "null"}, nil},
		{"json object", map[string]any{"value": `{"a":1}`}, map[string]any{"a": float64(1)}},
		{"json array", map[string]any{"value": `[1,2]`}, []any{float64(1), float64(2)}},
		{"non-string passthrough", map[string]any{"value": 42}, 42},
		{"empty string", map[string]any{"value": ""}, ""},
		{"val alias", map[string]any{"val": "x"}, "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := run(t, Const, c.in)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestConst_MissingValue(t *testing.T) {
	t.Parallel()
	_, err := run(t, Const, map[string]any{})
	ktest.RequireErrorKind(t, err, xerr.KindBadRequest)
}

// ── noop ─────────────────────────────────────────────────────────

func TestNoop_PassThrough(t *testing.T) {
	t.Parallel()
	in := map[string]any{"x": 1}
	got, err := run(t, Noop, in)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual[any](t, got, in)
}

// ── debug ────────────────────────────────────────────────────────

func TestDebug_PassThrough(t *testing.T) {
	t.Parallel()
	got, err := run(t, Debug, map[string]any{"x": 1})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual[any](t, got, map[string]any{"x": 1})
}

// ── fail ─────────────────────────────────────────────────────────

func TestFail_DefaultKindInternal(t *testing.T) {
	t.Parallel()
	_, err := run(t, Fail, map[string]any{"message": "boom"})
	ktest.RequireErrorKind(t, err, xerr.KindInternal)
}

func TestFail_ExplicitKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind string
		want xerr.Kind
	}{
		{"Validation", xerr.KindValidation},
		{"BadRequest", xerr.KindBadRequest},
		{"Unauthorized", xerr.KindUnauthorized},
		{"Forbidden", xerr.KindForbidden},
		{"NotFound", xerr.KindNotFound},
		{"Conflict", xerr.KindConflict},
		{"Timeout", xerr.KindTimeout},
		{"Unavailable", xerr.KindUnavailable},
		{"TooManyRequests", xerr.KindTooManyRequests},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			t.Parallel()
			_, err := run(t, Fail, map[string]any{"message": "x", "kind": c.kind})
			ktest.RequireErrorKind(t, err, c.want)
		})
	}
}

func TestFail_DefaultMessage(t *testing.T) {
	t.Parallel()
	_, err := run(t, Fail, map[string]any{})
	ktest.RequireErrorContains(t, err, "pipeline deliberately aborted")
}

// ── pick ─────────────────────────────────────────────────────────

func TestPick_ExtractsNested(t *testing.T) {
	t.Parallel()
	got, err := run(t, Pick, map[string]any{
		"field": "a.b.c",
		"a":     map[string]any{"b": map[string]any{"c": 42}},
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, 42)
}

func TestPick_FieldMissing(t *testing.T) {
	t.Parallel()
	_, err := run(t, Pick, map[string]any{"field": "nope", "a": 1})
	ktest.RequireErrorKind(t, err, xerr.KindNotFound)
}

func TestPick_NoFieldArg(t *testing.T) {
	t.Parallel()
	_, err := run(t, Pick, map[string]any{})
	ktest.RequireErrorKind(t, err, xerr.KindBadRequest)
}

// ── wrap ─────────────────────────────────────────────────────────

func TestWrap_UnderKey(t *testing.T) {
	t.Parallel()
	got, err := run(t, Wrap, map[string]any{"key": "data", "x": 1})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual[any](t, got, map[string]any{"data": map[string]any{"x": 1}})
}

func TestWrap_NoKey(t *testing.T) {
	t.Parallel()
	_, err := run(t, Wrap, map[string]any{"x": 1})
	ktest.RequireErrorKind(t, err, xerr.KindBadRequest)
}

// ── env ──────────────────────────────────────────────────────────

func TestEnv_ReadsValue(t *testing.T) {
	t.Setenv("NEXSS_TEST_ENV", "hello")
	got, err := run(t, Env, map[string]any{"name": "NEXSS_TEST_ENV"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, "hello")
}

func TestEnv_MissingOptional(t *testing.T) {
	t.Parallel()
	got, err := run(t, Env, map[string]any{"name": "NEXSS_TEST_MISSING_XYZ"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, "")
}

func TestEnv_MissingRequired(t *testing.T) {
	t.Parallel()
	_, err := run(t, Env, map[string]any{"name": "NEXSS_TEST_MISSING_XYZ", "required": "true"})
	ktest.RequireErrorKind(t, err, xerr.KindNotFound)
}

// ── uuid ─────────────────────────────────────────────────────────

func TestUUID_GeneratesString(t *testing.T) {
	t.Parallel()
	got, err := run(t, UUID, nil)
	ktest.RequireNoError(t, err)

	s, ok := got.(string)
	ktest.RequireCondition(t, ok, "uuid result is not a string")
	ktest.RequireEqual(t, len(s), 36)
	ktest.RequireEqual(t, strings.Count(s, "-"), 4)
}

func TestUUID_MergesUnderKey(t *testing.T) {
	t.Parallel()
	got, err := run(t, UUID, map[string]any{"as": "trace_id", "x": 1})
	ktest.RequireNoError(t, err)

	m, ok := got.(map[string]any)
	ktest.RequireCondition(t, ok, "result type = %T, want map", got)
	ktest.RequireEqual(t, m["x"], 1)
	ktest.RequireCondition(t, m["trace_id"] != nil, "trace_id not set")
}

// ── json.clean ───────────────────────────────────────────────────

func TestJSONClean_StripsFence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", `{"a":1}`, `{"a":1}`},
		{"fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"preamble", `Here is the JSON: {"a":1}`, `{"a":1}`},
		{"trailing prose", `{"a":1} Hope that helps!`, `{"a":1}`},
		{"array", `[1,2,3]`, `[1,2,3]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := run(t, JSONClean, c.in)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual[any](t, got, c.want)
		})
	}
}

func TestJSONClean_MapPassthrough(t *testing.T) {
	t.Parallel()
	got, err := run(t, JSONClean, map[string]any{
		"content": "```json\n{\"a\":1}\n```",
		"model":   "x",
	})
	ktest.RequireNoError(t, err)

	m, ok := got.(map[string]any)
	ktest.RequireCondition(t, ok, "result type = %T, want map", got)
	ktest.RequireEqual(t, m["content"], `{"a":1}`)
	ktest.RequireEqual(t, m["model"], "x")
}

// ── helpers ──────────────────────────────────────────────────────

func TestReadStringArg(t *testing.T) {
	t.Parallel()
	in := map[string]any{"k": "v", "num": 42}
	ktest.RequireEqual(t, readStringArg(in, "k"), "v")
	ktest.RequireEqual(t, readStringArg(in, "num"), "")
	ktest.RequireEqual(t, readStringArg(in, "missing"), "")
	ktest.RequireEqual(t, readStringArg("not-a-map", "k"), "")
}

func TestIsTruthyArg(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		val  any
		want bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"string true", "true", true},
		{"string 1", "1", true},
		{"string false", "false", false},
		{"number", 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := isTruthyArg(map[string]any{"k": c.val}, "k")
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestLookupDottedPath(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"a": map[string]any{"b": map[string]any{"c": 42}},
	}

	got, ok := lookupDottedPath(in, "a.b.c")
	ktest.RequireCondition(t, ok, "path not found")
	ktest.RequireEqual(t, got, 42)

	_, ok = lookupDottedPath(in, "a.missing")
	ktest.RequireCondition(t, !ok, "missing path should not resolve")

	root, ok := lookupDottedPath(in, "")
	ktest.RequireCondition(t, ok, "root not returned")

	rootMap, ok := root.(map[string]any)
	ktest.RequireCondition(t, ok, "root type = %T, want map[string]any", root)
	ktest.RequireEqual(t, rootMap, in)
}
