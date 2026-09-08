package flow

import "context"

// ApprovalGate defines an interface for human-in-the-loop or policy approvals.
type ApprovalGate interface {
	Check(ctx context.Context, actionName string, payload string, token string) error
}

// CostReporter is an interface that nodes can implement to report runtime costs to the ledger.
type CostReporter interface {
	CostMicros() int64
}
