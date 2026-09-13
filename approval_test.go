package flow_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow"
)

func TestApprovalToken_RoundTrip(t *testing.T) {
	ctx := flow.WithApprovalToken(context.Background(), "tok-abc")
	if got := flow.ApprovalTokenFrom(ctx); got != "tok-abc" {
		t.Fatalf("got %q, want tok-abc", got)
	}
}

func TestApprovalToken_MissingReturnsEmpty(t *testing.T) {
	if got := flow.ApprovalTokenFrom(context.Background()); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestApprovalToken_Overwrite(t *testing.T) {
	ctx := flow.WithApprovalToken(context.Background(), "first")
	ctx = flow.WithApprovalToken(ctx, "second")
	if got := flow.ApprovalTokenFrom(ctx); got != "second" {
		t.Fatalf("got %q, want second", got)
	}
}
