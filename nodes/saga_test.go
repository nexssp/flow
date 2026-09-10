package nodes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

func TestDynamicSaga_SuccessfulExecution(t *testing.T) {
	t.Parallel()

	step1 := nodes.SagaStep{
		NodeID: "step1",
		Forward: action.New("step1.do", func(_ context.Context, in string) (string, error) {
			return in + "->step1", nil
		}).Build(),
	}

	step2 := nodes.SagaStep{
		NodeID: "step2",
		Forward: action.New("step2.do", func(_ context.Context, in string) (string, error) {
			return in + "->step2", nil
		}).Build(),
	}

	saga := nodes.NewDynamicSaga("test_saga", []nodes.SagaStep{step1, step2}).Build()

	res, err := saga.Do(context.Background(), "start")
	if err != nil {
		t.Fatalf("saga failed: %v", err)
	}

	if res != "start->step1->step2" {
		t.Fatalf("unexpected saga result: %v", res)
	}
}

func TestDynamicSaga_RollbackLIFOOnFailure(t *testing.T) {
	t.Parallel()

	var rolledBack []string

	step1 := nodes.SagaStep{
		NodeID: "flight.book",
		Forward: action.New("flight.book", func(_ context.Context, _ any) (string, error) {
			return "flight_confirmed", nil
		}).Build(),
		Compensate: action.New("flight.cancel", func(_ context.Context, _ any) (string, error) {
			rolledBack = append(rolledBack, "flight.cancel")

			return "cancelled", nil
		}).Build(),
	}

	step2 := nodes.SagaStep{
		NodeID: "hotel.book",
		Forward: action.New("hotel.book", func(_ context.Context, _ any) (string, error) {
			return "hotel_confirmed", nil
		}).Build(),
		Compensate: action.New("hotel.cancel", func(_ context.Context, _ any) (string, error) {
			rolledBack = append(rolledBack, "hotel.cancel")

			return "cancelled", nil
		}).Build(),
	}

	step3 := nodes.SagaStep{
		NodeID: "payment.charge",
		Forward: action.New("payment.charge", func(_ context.Context, _ any) (string, error) {
			return "", errors.New("insufficient funds")
		}).Build(),
	}

	saga := nodes.NewDynamicSaga("trip_booking", []nodes.SagaStep{step1, step2, step3}).Build()

	_, err := saga.Do(context.Background(), "payload")
	if err == nil {
		t.Fatal("expected saga failure at step 3")
	}

	if len(rolledBack) != 2 {
		t.Fatalf("expected 2 compensations, got %d", len(rolledBack))
	}

	// Must be LIFO order: hotel first, then flight
	if rolledBack[0] != "hotel.cancel" || rolledBack[1] != "flight.cancel" {
		t.Fatalf("expected LIFO rollback [hotel.cancel, flight.cancel], got %v", rolledBack)
	}
}
