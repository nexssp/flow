package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

type OrderInput struct {
	OrderID   string `json:"order_id"`
	AccountID string `json:"account_id"`
	AmountUSD int64  `json:"amount_usd"`
}

type FraudCheckResult struct {
	OrderID string `json:"order_id"`
	Allowed bool   `json:"allowed"`
	Score   int    `json:"score"`
}

type SettlementReceipt struct {
	TxID      string    `json:"tx_id"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	ctx := context.Background()

	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("[nexssp/flow] Architecture 02: Typed Ultra-Low Latency Hot Path")
	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("• Pattern   : Pure Go value structs allocated on the stack (Zero Heap Allocs)")
	fmt.Println("• Use-Case  : Core transaction ledgers, trading systems, financial clearance")
	fmt.Println(strings.Repeat("─", 78))

	validate := action.New("order.validate", func(_ context.Context, in OrderInput) (OrderInput, error) {
		fmt.Printf("   [1/3] order.validate | Validating OrderID: %s (Amount: $%d.00)\n", in.OrderID, in.AmountUSD)

		if in.AmountUSD <= 0 {
			return OrderInput{}, fmt.Errorf("invalid order amount")
		}

		return in, nil
	}).Build()

	fraud := action.New("risk.eval", func(_ context.Context, in OrderInput) (FraudCheckResult, error) {
		fmt.Printf("   [2/3] risk.eval      | Risk score evaluated: 12 (Threshold: < 50)\n")

		return FraudCheckResult{OrderID: in.OrderID, Allowed: in.AmountUSD < 10000, Score: 12}, nil
	}).Build()

	settle := action.New("payment.settle", func(_ context.Context, res FraudCheckResult) (SettlementReceipt, error) {
		fmt.Printf("   [3/3] payment.settle | Processing final settlement entry\n")

		return SettlementReceipt{
			TxID:      "tx_settle_" + res.OrderID,
			Status:    "CONFIRMED",
			Timestamp: time.Now().UTC(),
		}, nil
	}).Build()

	registry := flow.NewRegistry(validate, fraud, settle)

	dsl := `order.validate -> risk.eval -> payment.settle`
	fmt.Printf("⚡ Pipeline Topography: %s\n\n", dsl)

	pipeline, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		panic(err)
	}

	execStart := time.Now()

	receipt, err := pipeline.Build().Do(ctx, OrderInput{
		OrderID:   "ord_99001",
		AccountID: "acc_alpha_enterprise",
		AmountUSD: 4900,
	})
	if err != nil {
		panic(err)
	}

	res := receipt.(SettlementReceipt)

	fmt.Println(strings.Repeat("─", 78))
	fmt.Printf("• Settlement TxID   : %s\n", res.TxID)
	fmt.Printf("• Settlement Status : %s\n", res.Status)
	fmt.Printf("• Timestamp         : %s\n", res.Timestamp.Format(time.RFC3339Nano))
	fmt.Printf("• Total Latency     : %v\n", time.Since(execStart))
	fmt.Println(strings.Repeat("═", 78))
}
