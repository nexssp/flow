package bundle

import (
	"context"
	"errors"
	"fmt"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "ledger_tools"

func init() { compiler.Register(ID, Bundle) }

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name: "ledger",
			Actions: []action.AnyAction{
				action.New("ledger.reconcile", reconcile).
					Description("Match bank rows to payment records and produce audit exceptions").Build(),
			},
		}},
	}
}

func reconcile(_ context.Context, in map[string]any) (map[string]any, error) {
	payments, ok := rows(in["payments"])
	if !ok || len(payments) == 0 {
		return nil, errors.New("ledger.reconcile: payments must contain at least one row")
	}
	bankRows, ok := rows(in["bank_rows"])
	if !ok || len(bankRows) == 0 {
		return nil, errors.New("ledger.reconcile: bank_rows must contain at least one row")
	}

	bankByRef := make(map[string]int64, len(bankRows))
	bankTotal := int64(0)
	for _, row := range bankRows {
		ref, cents, err := parseRow(row)
		if err != nil {
			return nil, err
		}
		bankByRef[ref] = cents
		bankTotal += cents
	}

	paymentTotal := int64(0)
	matched := 0
	usedBankRefs := make(map[string]bool, len(payments))
	exceptions := make([]map[string]any, 0)
	for _, row := range payments {
		ref, cents, err := parseRow(row)
		if err != nil {
			return nil, err
		}
		paymentTotal += cents
		bankCents, found := bankByRef[ref]
		if found {
			usedBankRefs[ref] = true
		}
		if found && bankCents == cents {
			matched++
			continue
		}
		exceptions = append(exceptions, map[string]any{
			"type":          "payment_mismatch_or_missing",
			"reference":     ref,
			"payment_cents": cents,
			"bank_cents":    bankCents,
		})
	}
	for _, row := range bankRows {
		ref, cents, err := parseRow(row)
		if err != nil {
			return nil, err
		}
		if !usedBankRefs[ref] {
			exceptions = append(exceptions, map[string]any{
				"type":       "bank_only",
				"reference":  ref,
				"bank_cents": cents,
			})
		}
	}

	return map[string]any{
		"batch_id":            in["batch_id"],
		"currency":            in["currency"],
		"payment_total_cents": paymentTotal,
		"bank_total_cents":    bankTotal,
		"variance_cents":      bankTotal - paymentTotal,
		"matched_count":       matched,
		"exception_count":     len(exceptions),
		"exceptions":          exceptions,
	}, nil
}

func rows(value any) ([]map[string]any, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, row)
	}
	return out, true
}

func parseRow(row map[string]any) (reference string, cents int64, err error) {
	var ok bool
	reference, ok = row["reference"].(string)
	if !ok || reference == "" {
		return "", 0, errors.New("ledger.reconcile: each row needs a reference")
	}
	cents, ok = integer(row["cents"])
	if !ok || cents < 0 {
		return "", 0, fmt.Errorf("ledger.reconcile: row %s needs a non-negative integer cents value", reference)
	}
	return reference, cents, nil
}

func integer(value any) (int64, bool) {
	switch n := value.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n == float64(int64(n)) {
			return int64(n), true
		}
	}
	return 0, false
}
