package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// No defer cancel() – we call cancel manually on all exit paths.

	flightBook := action.New("flight.book", func(_ context.Context, _ any) (string, error) {
		fmt.Println("✈️  [Flight] Seat reserved successfully.")
		return "seat_12A", nil
	}).Build()
	flightCancel := action.New("flight.cancel", func(_ context.Context, _ any) (string, error) {
		fmt.Println("🚨 [Flight Rollback] Seat reservation cancelled.")
		return "refunded", nil
	}).Build()

	hotelReserve := action.New("hotel.reserve", func(_ context.Context, _ any) (string, error) {
		fmt.Println("🏨 [Hotel] Room blocked successfully.")
		return "room_402", nil
	}).Build()
	hotelRelease := action.New("hotel.release", func(_ context.Context, _ any) (string, error) {
		fmt.Println("🚨 [Hotel Rollback] Room release successful.")
		return "released", nil
	}).Build()

	inventoryLock := action.New("inventory.lock", func(_ context.Context, _ any) (string, error) {
		fmt.Println("📦 [Inventory] Stock locked.")
		return "locked", nil
	}).Build()
	inventoryUnlock := action.New("inventory.unlock", func(_ context.Context, _ any) (string, error) {
		fmt.Println("🚨 [Inventory Rollback] Stock returned to shelf.")
		return "unlocked", nil
	}).Build()

	paymentCharge := action.New("payment.charge", func(_ context.Context, _ any) (string, error) {
		fmt.Println("💳 [Payment] Attempting authorization...")
		time.Sleep(50 * time.Millisecond)
		return "", fmt.Errorf("payment declined: card blocked")
	}).Build()
	paymentRefund := action.New("payment.refund", func(_ context.Context, _ any) (string, error) {
		fmt.Println("🚨 [Payment Rollback] Transaction refunded.")
		return "refunded", nil
	}).Build()

	registry := flow.NewRegistry(
		flightBook, flightCancel,
		hotelReserve, hotelRelease,
		inventoryLock, inventoryUnlock,
		paymentCharge, paymentRefund,
	)

	requiredSagas := [][]string{
		{"flight.book", "flight.cancel"},
		{"hotel.reserve", "hotel.release"},
		{"inventory.lock", "inventory.unlock"},
		{"payment.charge", "payment.refund"},
	}
	for _, pair := range requiredSagas {
		if _, ok := registry.Get(pair[0]); !ok {
			fmt.Fprintf(os.Stderr, "❌ Preflight Failure: Action %q missing in registry\n", pair[0])
			cancel()
			os.Exit(1)
		}
		if _, ok := registry.Get(pair[1]); !ok {
			fmt.Fprintf(os.Stderr, "❌ Preflight Failure: Rollback Action %q missing in registry\n", pair[1])
			cancel()
			os.Exit(1)
		}
	}

	dsl := `
		( flight.book(rollback=flight.cancel) -> hotel.reserve(rollback=hotel.release) )
		&
		( inventory.lock(rollback=inventory.unlock) -> payment.charge(rollback=payment.refund) )
	`

	fmt.Println("⚡ Compiling parallel Saga pipeline...")
	builder, err := flow.CompileSaga(dsl, registry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Saga compilation failed: %v\n", err)
		cancel()
		os.Exit(1)
	}

	fmt.Println("\n🚀 Executing dual parallel Sagas...")
	fmt.Println("----------------------------------------------------------------")
	_, err = builder.Build().Do(ctx, "checkout_payload")
	fmt.Println("----------------------------------------------------------------")

	if err != nil {
		fmt.Printf("❌ Cascade Rollback Executed! Error: %v\n", err)
		cancel()
		os.Exit(1)
	}

	fmt.Println("✅ Traveling booking confirmed.")
	cancel()
}
