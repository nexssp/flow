package bundle

import (
	"context"
	"errors"
	"maps"
	"math"
	"strings"

	"github.com/nexssp/kernel/action"

	core "github.com/nexssp/flow/compiler"
)

const ID = "order_tools"

func init() { core.Register(ID, Bundle) }

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name: "order",
			Actions: []action.AnyAction{
				action.New("order.normalize", normalizeOrder).
					Description("Normalize order contact data and calculate a safe subtotal").Build(),
			},
		}},
	}
}

func normalizeOrder(_ context.Context, in map[string]any) (map[string]any, error) {
	email, ok := in["email"].(string)
	if !ok || strings.TrimSpace(email) == "" {
		return nil, errors.New("order.normalize: email is required")
	}
	items, ok := in["items"].([]any)
	if !ok || len(items) == 0 {
		return nil, errors.New("order.normalize: at least one item is required")
	}

	subtotal := 0.0
	for index, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("order.normalize: item must be an object")
		}
		price, priceOK := number(item["unit_price"])
		quantity, quantityOK := number(item["quantity"])
		if !priceOK || !quantityOK || price < 0 || quantity <= 0 {
			return nil, errors.New("order.normalize: item price and positive quantity are required")
		}
		if math.IsNaN(price) || math.IsInf(price, 0) || math.IsNaN(quantity) || math.IsInf(quantity, 0) {
			return nil, errors.New("order.normalize: item numbers must be finite")
		}
		_ = index // retain a clear validation loop if per-item diagnostics are added later
		subtotal += price * quantity
	}

	out := cloneMap(in)
	out["customer_email"] = strings.ToLower(strings.TrimSpace(email))
	out["subtotal"] = math.Round(subtotal*100) / 100
	out["currency"] = "USD"
	return out, nil
}

func number(value any) (float64, bool) {
	switch n := value.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+3)
	maps.Copy(out, in)
	return out
}
