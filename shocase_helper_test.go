package flow_test

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// stub returns a minimal deterministic action that ignores its input and
// returns value. Used to build pipelines where the interesting property
// is the graph topology, not the handler logic.
func stub(name, value string) action.AnyAction {
	return action.New(name, func(_ context.Context, _ any) (string, error) {
		return value, nil
	}).Build()
}

// ─── Read actions — tagged hot_path, must be 0 allocs/op ──────────────

func userGet() action.AnyAction {
	return action.New("user.get", func(_ context.Context, in map[string]any) (string, error) {
		if id, ok := in["id"].(string); ok && id != "" {
			return "user:" + id, nil
		}
		return "user:unknown", nil
	}).
		Description("Read a user by id").
		Tag("read", "hot_path").
		Route("catalog:user.get").
		Build()
}

func orderGet() action.AnyAction {
	return action.New("order.get", func(_ context.Context, in map[string]any) (string, error) {
		if id, ok := in["id"].(string); ok && id != "" {
			return "order:" + id, nil
		}
		return "order:unknown", nil
	}).
		Description("Read an order by id").
		Tag("read", "hot_path").
		Route("catalog:order.get").
		Build()
}

func productLookup() action.AnyAction {
	return action.New("product.lookup", func(_ context.Context, in map[string]any) (string, error) {
		if id, ok := in["id"].(string); ok && id != "" {
			return "product:" + id, nil
		}
		return "product:unknown", nil
	}).
		Description("Look up a product by id").
		Tag("read", "hot_path").
		Route("catalog:product.lookup").
		Build()
}

// ─── Mutating actions — tagged mutating, must be idempotent ──────────

func userCreate() action.AnyAction {
	return action.New("user.create", func(_ context.Context, _ map[string]any) (string, error) {
		return "user_created", nil
	}).
		Description("Create a user").
		Tag("write", "mutating").
		Route("catalog:user.create").
		Idempotent().
		Build()
}

func orderCreate() action.AnyAction {
	return action.New("order.create", func(_ context.Context, _ map[string]any) (string, error) {
		return "order_created", nil
	}).
		Description("Create an order").
		Tag("write", "mutating").
		Route("catalog:order.create").
		Idempotent().
		Build()
}

func orderRefund() action.AnyAction {
	return action.New("order.refund", func(_ context.Context, _ map[string]any) (string, error) {
		return "order_refunded", nil
	}).
		Description("Refund an order").
		Tag("write", "mutating").
		Route("catalog:order.refund").
		Idempotent().
		Build()
}

func paymentCharge() action.AnyAction {
	return action.New("payment.charge", func(_ context.Context, _ map[string]any) (string, error) {
		return "charged", nil
	}).
		Description("Charge a payment").
		Tag("write", "mutating").
		Route("catalog:payment.charge").
		Idempotent().
		Build()
}

func webhookDispatch() action.AnyAction {
	return action.New("webhook.dispatch", func(_ context.Context, _ map[string]any) (string, error) {
		return "dispatched", nil
	}).
		Description("Dispatch an outbound webhook").
		Tag("write", "mutating").
		Route("catalog:webhook.dispatch").
		Idempotent().
		Build()
}
