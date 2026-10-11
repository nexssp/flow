package redact

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestMap_MasksSensitiveKeys(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"user":          "alice",
		"password":      "s3cret",
		"api_key":       "AKIA...",
		"Authorization": "Bearer xyz",
		"oauth_token":   "gho_xxx",
	}
	got := Map(in)

	ktest.RequireEqual(t, got["user"], "alice")
	for _, key := range []string{"password", "api_key", "Authorization", "oauth_token"} {
		ktest.RequireEqual(t, got[key], Placeholder)
	}
}

func TestMap_CaseInsensitive(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"PASSWORD": "x",
		"Token":    "y",
		"ApiKey":   "z",
	}
	got := Map(in)
	for key := range in {
		ktest.RequireEqual(t, got[key], Placeholder)
	}
}

func TestMap_NestedMaps(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"payload": map[string]any{
			"user":   "bob",
			"secret": "s",
			"nested": map[string]any{"token": "t"},
		},
	}
	got := Map(in)

	payload, ok := got["payload"].(map[string]any)
	ktest.RequireCondition(t, ok, "payload = %T", got["payload"])
	ktest.RequireEqual(t, payload["user"], "bob")
	ktest.RequireEqual(t, payload["secret"], Placeholder)

	nested, ok := payload["nested"].(map[string]any)
	ktest.RequireCondition(t, ok, "nested = %T", payload["nested"])
	ktest.RequireEqual(t, nested["token"], Placeholder)
}

func TestMap_Slices(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"items": []any{
			map[string]any{"name": "a", "password": "p"},
			map[string]any{"name": "b", "password": "q"},
		},
	}
	got := Map(in)

	items, ok := got["items"].([]any)
	ktest.RequireCondition(t, ok, "items = %T", got["items"])
	for i, item := range items {
		m, ok := item.(map[string]any)
		ktest.RequireCondition(t, ok, "item %d = %T", i, item)
		ktest.RequireEqual(t, m["password"], Placeholder)
	}
}

func TestMap_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"password": "plain",
		"nested":   map[string]any{"token": "plain"},
	}
	_ = Map(original)

	ktest.RequireEqual(t, original["password"], "plain")
	nested, ok := original["nested"].(map[string]any)
	ktest.RequireCondition(t, ok, "nested is %T, want map[string]any", original["nested"])
	ktest.RequireEqual(t, nested["token"], "plain")
}

func TestMap_EmptyAndNil(t *testing.T) {
	t.Parallel()

	ktest.RequireLen(t, Map(nil), 0)
	ktest.RequireLen(t, Map(map[string]any{}), 0)
}

func TestMap_NonSensitiveValuesPassThrough(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"count":  42,
		"name":   "alice",
		"active": true,
		"tags":   []any{"a", "b"},
		"nested": map[string]any{"x": 1},
	}
	got := Map(in)

	ktest.RequireEqual(t, got["count"], 42)
	ktest.RequireEqual(t, got["name"], "alice")
	ktest.RequireEqual(t, got["active"], true)
}
