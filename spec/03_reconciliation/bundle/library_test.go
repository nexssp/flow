package bundle

import (
	"context"
	"strings"
	"testing"
)

func TestReconcileRejectsDuplicateReferences(t *testing.T) {
	t.Parallel()
	payment := map[string]any{"reference": "ref-1", "cents": int64(100)}
	bank := map[string]any{"reference": "ref-1", "cents": int64(100)}
	cases := []struct {
		name     string
		payments []any
		bankRows []any
		want     string
	}{
		{name: "duplicate payment", payments: []any{payment, payment}, bankRows: []any{bank}, want: "duplicate payment reference"},
		{name: "duplicate bank row", payments: []any{payment}, bankRows: []any{bank, bank}, want: "duplicate bank reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := reconcile(context.Background(), map[string]any{"payments": tc.payments, "bank_rows": tc.bankRows})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("reconcile() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestReconcileRejectsCentTotalOverflow(t *testing.T) {
	t.Parallel()
	maxCents := int64(1<<63 - 1)
	cases := []struct {
		name     string
		payments []any
		bankRows []any
	}{
		{
			name:     "bank total",
			payments: []any{map[string]any{"reference": "p", "cents": int64(0)}},
			bankRows: []any{map[string]any{"reference": "b1", "cents": maxCents}, map[string]any{"reference": "b2", "cents": int64(1)}},
		},
		{
			name:     "payment total",
			payments: []any{map[string]any{"reference": "p1", "cents": maxCents}, map[string]any{"reference": "p2", "cents": int64(1)}},
			bankRows: []any{map[string]any{"reference": "b", "cents": int64(0)}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := reconcile(context.Background(), map[string]any{"payments": tc.payments, "bank_rows": tc.bankRows})
			if err == nil || !strings.Contains(err.Error(), "overflows int64") {
				t.Fatalf("reconcile() error = %v, want cents overflow error", err)
			}
		})
	}
}
