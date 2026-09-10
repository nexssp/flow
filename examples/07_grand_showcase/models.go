package main

import (
	"fmt"
	"time"
)

type TenantContext struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	RequestID string `json:"request_id"`
}

type MigrationTask struct {
	Tenant     TenantContext `json:"tenant"`
	TicketID   string        `json:"ticket_id" validate:"required"`
	Prompt     string        `json:"prompt" validate:"required,min=10"`
	LegacyCode string        `json:"legacy_code" validate:"required"`
}

type SanitizedTask struct {
	SessionID     string `json:"session_id"`
	TenantID      string `json:"tenant_id"`
	TicketID      string `json:"ticket_id"`
	SanitizedGoal string `json:"sanitized_goal"`
	Codebase      string `json:"codebase"`
	RedactedPII   bool   `json:"redacted_pii"`
}

type PlannerBrief struct {
	SessionID     string `json:"session_id"`
	TenantID      string `json:"tenant_id"`
	TicketID      string `json:"ticket_id"`
	PlanSummary   string `json:"plan_summary"`
	SourceCode    string `json:"source_code"`
	EstimatedCost int64  `json:"estimated_cost"`
	BilledMicros  int64  `json:"cost_micros"`
}

func (b PlannerBrief) CostMicros() int64 { return b.BilledMicros }

type ArchitectSpec struct {
	SessionID  string `json:"session_id"`
	TenantID   string `json:"tenant_id"`
	TicketID   string `json:"ticket_id"`
	SourceCode string `json:"source_code"`
}

type ArchitectPlan struct {
	SessionID    string `json:"session_id"`
	TenantID     string `json:"tenant_id"`
	TicketID     string `json:"ticket_id"`
	ModernCode   string `json:"modern_code"`
	TokensPrompt int    `json:"tokens_prompt"`
	TokensComp   int    `json:"tokens_comp"`
	BilledMicros int64  `json:"cost_micros"`
}

func (a ArchitectPlan) CostMicros() int64 { return a.BilledMicros }

type SandboxReport struct {
	SessionID    string `json:"session_id"`
	TenantID     string `json:"tenant_id"`
	TicketID     string `json:"ticket_id"`
	TestOutput   string `json:"test_output"`
	CpuTimeMs    int64  `json:"cpu_time_ms"`
	BilledMicros int64  `json:"cost_micros"`
}

func (s SandboxReport) CostMicros() int64 { return s.BilledMicros }

type SecurityVerdict struct {
	SessionID     string `json:"session_id"`
	TenantID      string `json:"tenant_id"`
	TicketID      string `json:"ticket_id"`
	ScannerName   string `json:"scanner_name"`
	VerdictStatus string `json:"verdict_status"`
	BilledMicros  int64  `json:"cost_micros"`
}

func (s SecurityVerdict) CostMicros() int64 { return s.BilledMicros }

type SynthesisGather struct {
	SessionID  string `json:"session_id"`
	TenantID   string `json:"tenant_id"`
	TicketID   string `json:"ticket_id"`
	TestOutput string `json:"test_output"`
	SecStatus  string `json:"sec_status"`
}

type QualityVerdict struct {
	SessionID      string   `json:"session_id"`
	TenantID       string   `json:"tenant_id"`
	TicketID       string   `json:"ticket_id"`
	Approved       bool     `json:"approved"`
	Confidence     float64  `json:"confidence"`
	Explainability string   `json:"explainability"`
	HallucinationP float64  `json:"hallucination_probability"`
	AuditTrail     []string `json:"audit_trail"`
	BilledMicros   int64    `json:"cost_micros"`
}

func (q QualityVerdict) CostMicros() int64 { return q.BilledMicros }

type ModernizationOutcome struct {
	SessionID      string        `json:"session_id"`
	TenantID       string        `json:"tenant_id"`
	TicketID       string        `json:"ticket_id"`
	Status         string        `json:"status"`
	AuditSummary   string        `json:"audit_summary"`
	Duration       time.Duration `json:"duration"`
	TotalTokens    int           `json:"total_tokens"`
	TotalCostUSD   float64       `json:"total_cost_usd"`
	ProductionPass bool          `json:"production_pass"`
	CompletedAt    time.Time     `json:"completed_at"`
}

func (m ModernizationOutcome) String() string {
	return fmt.Sprintf("[%s] [%s] %s -> %s (Status: %s)", m.SessionID, m.TenantID, m.TicketID, m.AuditSummary, m.Status)
}
