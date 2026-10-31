package compiler

import (
	"context"
	"strings"
	"testing"
)

func TestBuildPayloadMergesArgumentMapSpread(t *testing.T) {
	t.Parallel()
	atom := &Atom{
		Name:       "runtime.const",
		ArgSpreads: []*Value{{Kind: ValueRef, Ref: "defaults"}},
		Args: map[string]*Value{
			"url": {Kind: ValueRef, Ref: "url"},
		},
	}
	payload, err := buildPayload(context.Background(), map[string]any{
		"defaults": map[string]any{"timeout": 5, "retries": 2},
		"url":      "/v1",
	}, atom)
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}
	if payload["timeout"] != 5 || payload["retries"] != 2 || payload["url"] != "/v1" {
		t.Fatalf("payload = %#v, want spread defaults and explicit url", payload)
	}
}

func TestBuildPayloadRejectsArgumentMapSpreadCollisions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		spreads []*Value
		args    map[string]*Value
		state   map[string]any
	}{
		{
			name:    "spread with explicit key",
			spreads: []*Value{{Kind: ValueRef, Ref: "defaults"}},
			args:    map[string]*Value{"url": {Kind: ValueRef, Ref: "url"}},
			state: map[string]any{
				"defaults": map[string]any{"url": "/defaults"},
				"url":      "/explicit",
			},
		},
		{
			name: "two spreads",
			spreads: []*Value{
				{Kind: ValueRef, Ref: "defaults"},
				{Kind: ValueRef, Ref: "overrides"},
			},
			state: map[string]any{
				"defaults":  map[string]any{"timeout": 5},
				"overrides": map[string]any{"timeout": 10},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := buildPayload(context.Background(), tc.state, &Atom{
				Name:       "runtime.const",
				ArgSpreads: tc.spreads,
				Args:       tc.args,
			})
			if err == nil || !strings.Contains(err.Error(), "collision") {
				t.Fatalf("buildPayload error = %v, want collision", err)
			}
		})
	}
}

func TestBuildPayloadRejectsNonObjectArgumentMapSpread(t *testing.T) {
	t.Parallel()
	_, err := buildPayload(context.Background(), map[string]any{"timeout": 5}, &Atom{
		Name:       "runtime.const",
		ArgSpreads: []*Value{{Kind: ValueRef, Ref: "timeout"}},
	})
	if err == nil || !strings.Contains(err.Error(), "must resolve to an object") {
		t.Fatalf("buildPayload error = %v, want non-object spread error", err)
	}
}

func TestArgumentMapSpreadDefersRequiredSchemaFieldValidation(t *testing.T) {
	t.Parallel()
	schema := []ArgFieldSpec{{Name: "handler", Kind: ArgCapabilityRef}}
	atom := &Atom{Name: "test.request"}
	if err := resolveArgSchema(nil, atom, schema); err == nil || !strings.Contains(err.Error(), "required capability reference") {
		t.Fatalf("schema error without spread = %v, want required capability reference", err)
	}
	atom.ArgSpreads = []*Value{{Kind: ValueRef, Ref: "defaults"}}
	if err := resolveArgSchema(nil, atom, schema); err != nil {
		t.Fatalf("schema error with runtime spread = %v, want deferred validation", err)
	}
}
