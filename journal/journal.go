package journal

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type BranchStatus string

const (
	BranchSelected BranchStatus = "selected"
	BranchSkipped  BranchStatus = "skipped"
)

type BranchRecord struct {
	RunID       string       `json:"run_id"`
	SourceNode  string       `json:"source_node"`
	From        string       `json:"from"`
	To          string       `json:"to"`
	When        string       `json:"when,omitempty"`
	Otherwise   bool         `json:"otherwise,omitempty"`
	Status      BranchStatus `json:"status"`
	Reason      string       `json:"reason"`
	DecisionSeq int          `json:"decision_seq"`
	CreatedAt   time.Time    `json:"created_at"`
}

// BranchJournal persists branching decisions for deterministic execution replay.
type BranchJournal interface {
	Get(ctx context.Context, runID, sourceNode string) ([]BranchRecord, bool, error)
	Put(ctx context.Context, runID, sourceNode string, records []BranchRecord) error
}

type MemoryBranchJournal struct {
	mu   sync.RWMutex
	data map[string][]BranchRecord
}

func NewMemoryBranchJournal() *MemoryBranchJournal {
	return &MemoryBranchJournal{data: make(map[string][]BranchRecord)}
}

func (j *MemoryBranchJournal) Get(_ context.Context, runID, sourceNode string) ([]BranchRecord, bool, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	records, ok := j.data[runID+"\x00"+sourceNode]
	if !ok {
		return nil, false, nil
	}
	return append([]BranchRecord(nil), records...), true, nil
}

func (j *MemoryBranchJournal) Put(_ context.Context, runID, sourceNode string, records []BranchRecord) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	key := runID + "\x00" + sourceNode
	if existing, ok := j.data[key]; ok {
		if !sameBranchRecords(existing, records) {
			return fmt.Errorf("graph: branch decision already exists with different records")
		}
		return nil
	}
	j.data[key] = append([]BranchRecord(nil), records...)
	return nil
}

func sameBranchRecords(a, b []BranchRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].RunID != b[i].RunID || a[i].SourceNode != b[i].SourceNode || a[i].From != b[i].From ||
			a[i].To != b[i].To || a[i].When != b[i].When || a[i].Otherwise != b[i].Otherwise || a[i].Status != b[i].Status {
			return false
		}
	}
	return true
}
