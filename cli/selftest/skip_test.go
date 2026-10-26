package selftest

import (
	"errors"
	"fmt"
	"testing"
)

type testSkippableError struct {
	reason string
}

func (e testSkippableError) Error() string              { return "optional dependency unavailable" }
func (e testSkippableError) SelfTestSkipReason() string { return e.reason }

func TestSelfTestSkipReason(t *testing.T) {
	const want = "python3 is not installed"
	got := selfTestSkipReason(fmt.Errorf("compile failed: %w", testSkippableError{reason: want}))
	if got != want {
		t.Fatalf("selfTestSkipReason() = %q; want %q", got, want)
	}
}

func TestSelfTestSkipReasonIgnoresOrdinaryErrors(t *testing.T) {
	if got := selfTestSkipReason(errors.New("unexpected compile error")); got != "" {
		t.Fatalf("selfTestSkipReason() = %q; want empty", got)
	}
}
