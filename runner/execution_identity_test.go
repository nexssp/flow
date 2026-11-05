package runner

import (
	"context"
	"testing"
)

func TestEnsureExecutionIdentityAddsDurableCountersOnlyWhenConfigured(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		cfg          Config
		wantCounters bool
	}{
		{name: "no durable stores"},
		{name: "journal", cfg: Config{Journal: &FileJournal{}}, wantCounters: true},
		{name: "idempotency store", cfg: Config{IdempotencyStore: &FileJournal{}}, wantCounters: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, executionID, err := ensureExecutionIdentity(context.Background(), testCase.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if executionID == "" {
				t.Fatal("execution ID is empty")
			}
			_, hasCounters := durableCountersContextKey.From(ctx)
			if hasCounters != testCase.wantCounters {
				t.Fatalf("durable counters present = %v, want %v", hasCounters, testCase.wantCounters)
			}
		})
	}
}
