package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"

	"github.com/nexssp/kernel/xerr"
)

var _ SuspensionStore = (*FileJournal)(nil)

// LoadApprovalSuspension returns the pending approval request for an execution.
func (j *FileJournal) LoadApprovalSuspension(ctx context.Context, executionID string) (ApprovalSuspension, bool, error) {
	if err := contextErr(ctx); err != nil {
		return ApprovalSuspension{}, false, err
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if err := j.ensureOpenLocked(); err != nil {
		return ApprovalSuspension{}, false, err
	}
	data, err := j.root.ReadFile(fileKeyPath("approvals", executionID))
	if errors.Is(err, fs.ErrNotExist) {
		return ApprovalSuspension{}, false, nil
	}
	if err != nil {
		return ApprovalSuspension{}, false, xerr.Unavailable("read approval suspension", err)
	}
	var suspension ApprovalSuspension
	if err := json.Unmarshal(data, &suspension); err != nil {
		return ApprovalSuspension{}, false, xerr.Internal("decode approval suspension", err)
	}
	if suspension.ExecutionID != executionID || suspension.Gate == "" || suspension.SourceHash == "" || suspension.InputHash == "" || suspension.ExpiresAt.IsZero() {
		return ApprovalSuspension{}, false, xerr.Internal("approval suspension record is incomplete")
	}
	suspension.Persisted = true
	return suspension, true, nil
}

// SaveApprovalSuspension atomically records the current pending approval gate.
func (j *FileJournal) SaveApprovalSuspension(ctx context.Context, suspension ApprovalSuspension) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if suspension.ExecutionID == "" || suspension.Gate == "" || suspension.SourceHash == "" || suspension.InputHash == "" || suspension.ExpiresAt.IsZero() {
		return xerr.BadRequest("approval suspension record is incomplete")
	}
	suspension.Persisted = false
	data, err := json.Marshal(suspension)
	if err != nil {
		return xerr.Internal("encode approval suspension", err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.ensureOpenLocked(); err != nil {
		return err
	}
	if err := j.root.WriteFileAtomic(fileKeyPath("approvals", suspension.ExecutionID), data, 0o600); err != nil {
		return xerr.Unavailable("write approval suspension", err)
	}
	return nil
}

// ClearApprovalSuspension removes a pending approval record after the resumed
// execution completes successfully.
func (j *FileJournal) ClearApprovalSuspension(ctx context.Context, executionID string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.ensureOpenLocked(); err != nil {
		return err
	}
	if err := j.root.Remove(fileKeyPath("approvals", executionID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return xerr.Unavailable("remove approval suspension", err)
	}
	return nil
}
