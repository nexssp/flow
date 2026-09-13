package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nexssp/kernel/xerr"
)

// ErrCorrupt marks a snapshot file that exists but cannot be decoded.
// Callers (and the journal itself) treat this as "fall back", not "fail".
var ErrCorrupt = errors.New("snapshot: corrupt")

// Snapshot captures the resumable state of a run at a given step.
type Snapshot struct {
	RunID       string         `json:"run_id"`
	StepIndex   int            `json:"step_index"`
	StateData   map[string]any `json:"state_data"`
	SpentMicros int64          `json:"spent_micros"`
	Timestamp   time.Time      `json:"timestamp"`
}

// SnapshotJournal persists and recovers run snapshots.
type SnapshotJournal interface {
	Save(ctx context.Context, s Snapshot) error
	Recover(ctx context.Context, runID string) (*Snapshot, bool, error)
}

// FileSnapshotJournal is a durable, filesystem-backed SnapshotJournal.
//
// Layout:
//
//	<baseDir>/<runID>/step_00042.json   immutable per-step snapshots
//	<baseDir>/<runID>/latest.json       most recent snapshot (pointer)
//
// Every write is atomic + durable: temp file -> fsync -> rename -> fsync(dir).
// Readers take no lock: rename() is atomic, so Recover observes either the
// previous or the new file, never a torn one. This keeps the read path cheap
// and eliminates reader/writer contention.
//
// The writer mutex serializes the shared latest.json rename target. Under
// contention this is the correct trade-off: the critical section is dominated
// by kernel I/O, not the lock itself.
type FileSnapshotJournal struct {
	baseDir string
	mu      sync.Mutex
	bufPool sync.Pool // *bytes.Buffer
}

// NewFileSnapshotJournal creates or opens a journal rooted at baseDir.
func NewFileSnapshotJournal(baseDir string) (*FileSnapshotJournal, error) {
	if baseDir == "" {
		return nil, errors.New("snapshot: empty base dir")
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("snapshot dir: %w", err)
	}

	j := &FileSnapshotJournal{baseDir: baseDir}
	j.bufPool.New = func() any { return new(bytes.Buffer) }

	return j, nil
}

// Save durably persists s. Safe for concurrent use.
func (f *FileSnapshotJournal) Save(ctx context.Context, s Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := validateRunID(s.RunID); err != nil {
		return err
	}

	buf := f.bufPool.Get().(*bytes.Buffer)
	defer f.bufPool.Put(buf)

	buf.Reset()

	if err := json.NewEncoder(buf).Encode(s); err != nil {
		return xerr.Internal("marshal snapshot failed", err)
	}

	data := buf.Bytes() // borrowed from buf; must not outlive this call

	f.mu.Lock()
	defer f.mu.Unlock()

	dir := filepath.Join(f.baseDir, s.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("snapshot: mkdir session: %w", err)
	}

	// Per-step file first, then the latest pointer. If we crash in between,
	// Recover falls back to the highest step_*.json. No data loss.
	if err := writeAtomic(filepath.Join(dir, stepName(s.StepIndex)), data); err != nil {
		return err
	}

	return writeAtomic(filepath.Join(dir, latestName), data)
}

// Recover loads the most recent snapshot for runID.
// The bool is false when no snapshot exists (not an error).
// Missing or corrupt latest.json transparently falls back to the highest step.
func (f *FileSnapshotJournal) Recover(ctx context.Context, runID string) (*Snapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	if err := validateRunID(runID); err != nil {
		return nil, false, err
	}

	dir := filepath.Join(f.baseDir, runID)

	s, err := readSnapshot(filepath.Join(dir, latestName))
	switch {
	case err == nil:
		return s, true, nil
	case errors.Is(err, os.ErrNotExist), errors.Is(err, ErrCorrupt):
		// fall through to scan
	default:
		return nil, false, err
	}

	return recoverHighestStep(dir)
}

// --- internals -------------------------------------------------------------

const latestName = "latest.json"

func stepName(i int) string {
	if i < 0 {
		i = 0
	}

	return fmt.Sprintf("step_%05d.json", i)
}

func readSnapshot(path string) (*Snapshot, error) {
	fp, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fp.Close()

	var s Snapshot
	if err := json.NewDecoder(fp).Decode(&s); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrCorrupt, path, err)
	}

	return &s, nil
}

// recoverHighestStep scans dir for step_*.json and returns the highest index.
func recoverHighestStep(dir string) (*Snapshot, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}

		return nil, false, err
	}

	const (
		prefix = "step_"
		suffix = ".json"
	)

	best, bestName := -1, ""

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}

		idx, perr := strconv.Atoi(name[len(prefix) : len(name)-len(suffix)])
		if perr != nil || idx <= best {
			continue
		}

		best, bestName = idx, name
	}

	if best < 0 {
		return nil, false, nil
	}

	s, err := readSnapshot(filepath.Join(dir, bestName))
	if err != nil {
		return nil, false, err
	}

	return s, true, nil
}

// writeAtomic writes data to path atomically and durably:
// write temp -> fsync temp -> rename -> fsync dir.
// The dir fsync is what makes the rename itself survive a crash.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("snapshot: create tmp: %w", err)
	}

	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)

		return fmt.Errorf("snapshot: write tmp: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)

		return fmt.Errorf("snapshot: fsync tmp: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)

		return fmt.Errorf("snapshot: close tmp: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)

		return fmt.Errorf("snapshot: rename: %w", err)
	}

	// fsync the parent directory so the rename is durable. Best-effort:
	// a failure here does not invalidate the (already visible) new file.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	return nil
}

func validateRunID(id string) error {
	if id == "" {
		return errors.New("snapshot: empty run id")
	}

	if id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("snapshot: invalid run id %q", id)
	}

	return nil
}
