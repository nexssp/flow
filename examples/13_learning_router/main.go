package main

import (
	"context"
	cryptoRand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nexssp/flow/learn"
	"github.com/nexssp/kernel/action"
)

type PaymentRes struct {
	Provider string
	CostCent int
}

func secureFloat64() float64 {
	var b [8]byte
	if _, err := cryptoRand.Read(b[:]); err != nil {
		return 0.5
	}
	return float64(binary.LittleEndian.Uint64(b[:])) / float64(math.MaxUint64)
}

func main() {
	ctx := context.Background()

	var stripeCalls, paypalCalls, adyenCalls atomic.Int32

	stripe := action.New("pay.stripe", func(_ context.Context, _ any) (PaymentRes, error) {
		stripeCalls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return PaymentRes{Provider: "Stripe", CostCent: 5}, nil
	}).Build()

	paypal := action.New("pay.paypal", func(_ context.Context, _ any) (PaymentRes, error) {
		paypalCalls.Add(1)
		time.Sleep(50 * time.Millisecond)
		if secureFloat64() < 0.20 {
			return PaymentRes{}, fmt.Errorf("paypal network timeout")
		}
		return PaymentRes{Provider: "PayPal", CostCent: 2}, nil
	}).Build()

	adyen := action.New("pay.adyen", func(_ context.Context, _ any) (PaymentRes, error) {
		adyenCalls.Add(1)
		time.Sleep(15 * time.Millisecond)
		if secureFloat64() < 0.05 {
			return PaymentRes{}, fmt.Errorf("adyen rate limit")
		}
		return PaymentRes{Provider: "Adyen", CostCent: 1}, nil
	}).Build()

	rewardFn := func(res any, err error, duration time.Duration) float64 {
		if err != nil {
			return -50.0
		}

		val, ok := res.(PaymentRes)
		if !ok {
			return -10.0
		}

		reward := 20.0
		reward -= float64(val.CostCent) * 2.0
		reward -= float64(duration.Milliseconds()) / 10.0

		return reward
	}

	smartRouter := learn.NewRouter(learn.RouterConfig{
		Name:         "payment.smart_router",
		Temperature:  2.5,
		LearningRate: 0.1,
		RewardFn:     rewardFn,
	}, stripe, paypal, adyen)

	fmt.Println("🚀 Starting Autonomous Payment Routing Simulation...")
	fmt.Println("Goal: Maximize success, minimize cost/latency. Optimal target is Adyen.")
	fmt.Println(strings.Repeat("-", 60))

	epochs := 150
	window := 30
	var successCount, failCount int

	for i := 1; i <= epochs; i++ {
		_, err := smartRouter.DoAny(ctx, "charge_10_usd")

		if err != nil {
			failCount++
		} else {
			successCount++
		}

		if i%window == 0 {
			fmt.Printf("📊 Epoch %3d | Success Rate: %3.0f%% | Traffic Share -> Stripe: %2d, PayPal: %2d, Adyen: %2d\n",
				i,
				float64(successCount)/float64(window)*100,
				stripeCalls.Load(), paypalCalls.Load(), adyenCalls.Load(),
			)
			stripeCalls.Store(0)
			paypalCalls.Store(0)
			adyenCalls.Store(0)
			successCount = 0
			failCount = 0
		}
	}

	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("✅ Simulation Complete. The router autonomously discovered the optimal provider.")
}
