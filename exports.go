package flow

import (
	"database/sql"

	"github.com/nexssp/flow/governance"
	"github.com/nexssp/flow/journal"
	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/flow/telemetry"
	"github.com/nexssp/kernel/action"
)

// ── Governance Aliases ───────────────────────────────────────────────────────

type CostLedger = governance.CostLedger
type MemoryCostLedger = governance.MemoryCostLedger
type CostEvent = governance.CostEvent
type CostUsage = governance.CostUsage
type BudgetCheck = governance.BudgetCheck

func NewMemoryCostLedger() *MemoryCostLedger {
	return governance.NewMemoryCostLedger()
}

func AsCostHook(ledger CostLedger, estimatedCostMicros int64, budgetLimitMicros int64) action.AnyHook {
	return governance.AsCostHook(ledger, estimatedCostMicros, budgetLimitMicros)
}

// ── Journal Aliases ──────────────────────────────────────────────────────────

type BranchStatus = journal.BranchStatus
type BranchRecord = journal.BranchRecord
type BranchJournal = journal.BranchJournal
type MemoryBranchJournal = journal.MemoryBranchJournal
type SQLBranchJournal = journal.SQLBranchJournal

const (
	BranchSelected = journal.BranchSelected
	BranchSkipped  = journal.BranchSkipped
)

func NewMemoryBranchJournal() *MemoryBranchJournal {
	return journal.NewMemoryBranchJournal()
}

func NewSQLBranchJournal(db *sql.DB) *SQLBranchJournal {
	return journal.NewSQLBranchJournal(db)
}

// ── Nodes Aliases ────────────────────────────────────────────────────────────

type PromptConfig = nodes.PromptConfig
type SupervisorReq = nodes.SupervisorReq
type SupervisorRes = nodes.SupervisorRes
type ChildTask = nodes.ChildTask
type ChildResult = nodes.ChildResult

func NewPromptNode(cfg nodes.PromptConfig) action.AnyAction {
	return nodes.NewPromptNode(cfg)
}

func NewSupervisorNode(name string, compiler nodes.PipelineCompiler) action.AnyAction {
	return nodes.NewSupervisorNode(name, compiler)
}

// ── Telemetry Aliases ────────────────────────────────────────────────────────

type LockFreeRingBuffer = telemetry.LockFreeRingBuffer
type EventSlot = telemetry.EventSlot
type HotPathExecutor = telemetry.HotPathExecutor

func NewLockFreeRingBuffer() *LockFreeRingBuffer {
	return telemetry.NewLockFreeRingBuffer()
}

func NewHotPathExecutor(ring *LockFreeRingBuffer) *HotPathExecutor {
	return telemetry.NewHotPathExecutor(ring)
}
