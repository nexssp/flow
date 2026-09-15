package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nexssp/kernel/xerr"
)

type Checkpoint struct {
	RunID       string         `json:"run_id"`
	Flow        string         `json:"flow"`
	FlowHash    string         `json:"flow_hash"`
	Layer       int            `json:"layer"`
	SavedAt     time.Time      `json:"saved_at"`
	SpentMicros int64          `json:"spent_micros"`
	State       map[string]any `json:"state"`
}

type CheckpointStore interface {
	Save(ctx context.Context, cp Checkpoint) error
	Load(ctx context.Context, runID string) (Checkpoint, bool, error)
	Delete(ctx context.Context, runID string) error
}

type FileCheckpointStore struct {
	baseDir string
}

func NewFileCheckpointStore(baseDir string) (*FileCheckpointStore, error) {
	if baseDir == "" {
		return nil, xerr.BadRequest("checkpoint: baseDir required")
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, xerr.Internal("checkpoint: create baseDir", err)
	}

	return &FileCheckpointStore{baseDir: baseDir}, nil
}

func (s *FileCheckpointStore) path(runID string) string {
	return filepath.Join(s.baseDir, runID+".json")
}

func (s *FileCheckpointStore) Save(ctx context.Context, cp Checkpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return xerr.Internal("checkpoint: marshal", err)
	}

	target := s.path(cp.RunID)

	tmp := target + ".tmp"
	// 0600: checkpoints carry full state (messages, tool args, prompts).
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return xerr.Internal("checkpoint: write tmp", err)
	}

	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)

		return xerr.Internal("checkpoint: rename", err)
	}

	return nil
}

func (s *FileCheckpointStore) Load(ctx context.Context, runID string) (Checkpoint, bool, error) {
	if err := ctx.Err(); err != nil {
		return Checkpoint{}, false, err
	}

	data, err := os.ReadFile(s.path(runID))
	if err != nil {
		if os.IsNotExist(err) {
			return Checkpoint{}, false, nil
		}

		return Checkpoint{}, false, xerr.Internal("checkpoint: read", err)
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return Checkpoint{}, false, xerr.Internal("checkpoint: unmarshal", err)
	}

	return cp, true, nil
}

func (s *FileCheckpointStore) Delete(ctx context.Context, runID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	err := os.Remove(s.path(runID))
	if err != nil && !os.IsNotExist(err) {
		return xerr.Internal("checkpoint: delete", err)
	}

	return nil
}

type MemoryCheckpointStore struct {
	mu sync.RWMutex
	m  map[string]Checkpoint
}

func NewMemoryCheckpointStore() *MemoryCheckpointStore {
	return &MemoryCheckpointStore{m: make(map[string]Checkpoint)}
}

func (s *MemoryCheckpointStore) Save(ctx context.Context, cp Checkpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	s.m[cp.RunID] = cp
	s.mu.Unlock()

	return nil
}

func (s *MemoryCheckpointStore) Load(ctx context.Context, runID string) (Checkpoint, bool, error) {
	if err := ctx.Err(); err != nil {
		return Checkpoint{}, false, err
	}

	s.mu.RLock()
	cp, ok := s.m[runID]
	s.mu.RUnlock()

	return cp, ok, nil
}

func (s *MemoryCheckpointStore) Delete(ctx context.Context, runID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.m, runID)
	s.mu.Unlock()

	return nil
}

func flowHash(dsl string) string {
	sum := sha256.Sum256([]byte(dsl))

	return hex.EncodeToString(sum[:16])
}

var (
	_ CheckpointStore = (*FileCheckpointStore)(nil)
	_ CheckpointStore = (*MemoryCheckpointStore)(nil)
)
