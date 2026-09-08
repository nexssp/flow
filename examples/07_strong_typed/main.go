package main

import (
	"context"
	"fmt"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

type ChargeOrderReq struct {
	CustomerID string         `json:"customer_id"`
	Amount     int64          `json:"amount_cents"`
	Currency   string         `json:"currency"`
	Metadata   map[string]any `json:"metadata,omitempty"` // dynamic escape hatch
}

type ChargeOrderRes struct {
	TxID     string `json:"tx_id"`
	Approved bool   `json:"approved"`
}

func main() {
	ctx := context.Background()

	createOrder := action.New("order.create", func(_ context.Context, _ any) (map[string]any, error) {
		return map[string]any{
			"customerID": "cus_123",
			"totalCents": 49900,
			"currency":   "usd",
			"metadata": map[string]any{
				"campaign": "black_friday",
			},
		}, nil
	}).Build()

	chargeOrder := action.New("stripe.charge", func(_ context.Context, req ChargeOrderReq) (ChargeOrderRes, error) {
		fmt.Printf("charging %s %.2f %s\n", req.CustomerID, float64(req.Amount)/100, req.Currency)
		return ChargeOrderRes{
			TxID:     "tx_998877",
			Approved: true,
		}, nil
	}).Build()

	notify := action.New("order.notify", func(_ context.Context, req map[string]any) (string, error) {
		return fmt.Sprintf("Order approved: %s", req["receipt_id"]), nil
	}).Build()

	registry := flow.NewRegistry(createOrder, chargeOrder, notify)

	dsl := `
		order.create
		-> { customer_id: customerID, amount_cents: totalCents, currency: currency, metadata: metadata }
		-> stripe.charge
		-> { receipt_id: tx_id, approved: approved }
		-> order.notify
	`

	builder, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		panic(err)
	}

	result, err := builder.Build().Do(ctx, nil)
	if err != nil {
		panic(err)
	}

	fmt.Println(result)
}
