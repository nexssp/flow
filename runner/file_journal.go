package runner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfs"
)

// FileJournal persists Kernel durable-step records and idempotency responses
// below one confined directory. Step and idempotency keys are hashed before
// they are used as relative paths. The same instance also coordinates
// idempotency claims among callers in this process; it is not a distributed
// lock service across separate processes.
type FileJournal struct {
	mu     sync.RWMutex
	root   *xfs.Root
	closed bool
	claims map[string]fileIdempotencyClaim
}

type fileIdempotencyRecord struct {
	Entry     action.IdempotencyEntry `json:"entry"`
	ExpiresAt time.Time               `json:"expires_at"`
}

type fileIdempotencyClaim struct {
	token       string
	requestHash string
	expiresAt   time.Time
}

var (
	_ action.Journal                 = (*FileJournal)(nil)
	_ action.IdempotencyCoordinator  = (*FileJournal)(nil)
	_ action.IdempotencyLeaseRenewer = (*FileJournal)(nil)
)

// NewFileJournal opens or creates a journal directory. It implements both
// action.Journal and action.IdempotencyCoordinator, so callers may assign the
// same instance to runner.Config.Journal and runner.Config.IdempotencyStore.
func NewFileJournal(dir string) (*FileJournal, error) {
	if dir == "" {
		return nil, xerr.BadRequest("file journal directory is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, xerr.BadRequest("resolve file journal directory", err)
	}
	if mkdirErr := os.MkdirAll(abs, 0o700); mkdirErr != nil {
		return nil, xerr.Internal("create file journal directory", mkdirErr)
	}
	root, err := xfs.OpenRoot(abs)
	if err != nil {
		return nil, xerr.Internal("open file journal directory", err)
	}
	for _, name := range []string{"steps", "idempotency", "approvals"} {
		if err := root.MkdirAll(name, 0o700); err != nil {
			_ = root.Close()
			return nil, xerr.Internal("create file journal subdirectory", err)
		}
	}
	return &FileJournal{root: root, claims: make(map[string]fileIdempotencyClaim)}, nil
}

// Close releases the rooted filesystem handle. The journal must not be used
// after Close; closing it more than once is safe.
func (j *FileJournal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	if err := j.root.Close(); err != nil {
		return xerr.Internal("close file journal", err)
	}
	return nil
}

// GetStep loads a previously recorded durable result. A missing record is a
// normal cache miss; filesystem errors are returned to Kernel and do not cause
// the step to run silently.
func (j *FileJournal) GetStep(ctx context.Context, executionID, stepName string) (data []byte, found bool, readErr error) {
	if err := contextErr(ctx); err != nil {
		return nil, false, err
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if err := j.ensureOpenLocked(); err != nil {
		return nil, false, err
	}
	data, err := j.root.ReadFile(fileKeyPath("steps", executionID, stepName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, xerr.Unavailable("read file journal step", err)
	}
	return data, true, nil
}

// RecordStep atomically writes one completed durable result.
func (j *FileJournal) RecordStep(ctx context.Context, executionID, stepName string, data []byte) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.ensureOpenLocked(); err != nil {
		return err
	}
	if err := j.root.WriteFileAtomic(fileKeyPath("steps", executionID, stepName), data, 0o600); err != nil {
		return xerr.Unavailable("write file journal step", err)
	}
	return nil
}

// Get implements action.IdempotencyStore. The Kernel's legacy Get contract
// cannot return an error; on a corrupt or unreadable persisted record it
// returns an invalid body so Kernel fails closed when decoding the cached
// response, rather than executing the action again.
func (j *FileJournal) Get(ctx context.Context, key string) (action.IdempotencyEntry, bool) {
	if err := contextErr(ctx); err != nil {
		return action.IdempotencyEntry{}, false
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if err := j.ensureOpenLocked(); err != nil {
		slog.ErrorContext(ctx, "file idempotency store unavailable", "error", err)
		return action.IdempotencyEntry{Body: []byte("{")}, true
	}
	record, found, err := j.readIdempotencyLocked(ctx, key)
	if err != nil {
		slog.ErrorContext(ctx, "file idempotency record read failed", "error", err)
		return action.IdempotencyEntry{Body: []byte("{")}, true
	}
	if !found || (!record.ExpiresAt.IsZero() && !time.Now().Before(record.ExpiresAt)) {
		return action.IdempotencyEntry{}, false
	}
	return record.Entry, true
}

// Set implements action.IdempotencyStore for compatibility with callers that
// use the basic store API. Kernel's IdempotencyMiddleware detects the
// coordinator methods below and uses Complete for normal executions, allowing
// persistence failures to be returned to the caller.
func (j *FileJournal) Set(ctx context.Context, key string, entry action.IdempotencyEntry, ttl time.Duration) {
	if err := contextErr(ctx); err != nil {
		return
	}
	if ttl <= 0 {
		ttl = action.DefaultIdempotencyTTL
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.ensureOpenLocked(); err != nil {
		slog.ErrorContext(ctx, "file idempotency store unavailable", "error", err)
		return
	}
	if err := j.writeIdempotencyLocked(ctx, key, fileIdempotencyRecord{
		Entry: entry, ExpiresAt: time.Now().Add(ttl),
	}); err != nil {
		slog.ErrorContext(ctx, "file idempotency record write failed", "error", err)
	}
}

// Claim coordinates duplicate idempotency keys within this FileJournal
// instance and checks for previously completed responses.
func (j *FileJournal) Claim(ctx context.Context, key, requestHash string, leaseTTL time.Duration) (action.IdempotencyClaim, error) {
	if err := contextErr(ctx); err != nil {
		return action.IdempotencyClaim{}, err
	}
	if leaseTTL <= 0 {
		leaseTTL = action.DefaultIdempotencyLeaseTTL
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.ensureOpenLocked(); err != nil {
		return action.IdempotencyClaim{}, err
	}
	now := time.Now()
	record, found, err := j.readIdempotencyLocked(ctx, key)
	if err != nil {
		return action.IdempotencyClaim{}, xerr.Unavailable("read file idempotency record", err)
	}
	if found && (record.ExpiresAt.IsZero() || now.Before(record.ExpiresAt)) {
		if record.Entry.RequestHash != "" && record.Entry.RequestHash != requestHash {
			return action.IdempotencyClaim{State: action.IdempotencyClaimConflict}, nil
		}
		return action.IdempotencyClaim{State: action.IdempotencyClaimCompleted, Entry: record.Entry}, nil
	}
	if claim, exists := j.claims[key]; exists {
		if now.Before(claim.expiresAt) {
			if claim.requestHash != requestHash {
				return action.IdempotencyClaim{State: action.IdempotencyClaimConflict}, nil
			}
			return action.IdempotencyClaim{State: action.IdempotencyClaimInProgress}, nil
		}
		delete(j.claims, key)
	}
	token, err := newFileJournalToken()
	if err != nil {
		return action.IdempotencyClaim{}, xerr.Internal("create file idempotency claim", err)
	}
	j.claims[key] = fileIdempotencyClaim{
		token: token, requestHash: requestHash, expiresAt: now.Add(leaseTTL),
	}
	return action.IdempotencyClaim{State: action.IdempotencyClaimAcquired, Token: token}, nil
}

// Complete persists the response and releases the active claim.
func (j *FileJournal) Complete(ctx context.Context, key, token string, entry action.IdempotencyEntry, ttl time.Duration) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = action.DefaultIdempotencyTTL
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.ensureOpenLocked(); err != nil {
		return err
	}
	claim, found := j.claims[key]
	if !found || claim.token != token || !time.Now().Before(claim.expiresAt) {
		return xerr.Conflict("file idempotency claim is missing or expired")
	}
	if entry.RequestHash == "" {
		entry.RequestHash = claim.requestHash
	}
	if err := j.writeIdempotencyLocked(ctx, key, fileIdempotencyRecord{
		Entry: entry, ExpiresAt: time.Now().Add(ttl),
	}); err != nil {
		return xerr.Unavailable("write file idempotency response", err)
	}
	delete(j.claims, key)
	return nil
}

// Release drops an uncompleted idempotency claim.
func (j *FileJournal) Release(ctx context.Context, key, token string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.ensureOpenLocked(); err != nil {
		return err
	}
	claim, found := j.claims[key]
	if !found {
		return nil
	}
	if claim.token != token {
		return xerr.Conflict("file idempotency claim token does not match")
	}
	delete(j.claims, key)
	return nil
}

// RenewClaim extends an active claim's lease while a long action is running.
func (j *FileJournal) RenewClaim(ctx context.Context, key, token string, leaseTTL time.Duration) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if leaseTTL <= 0 {
		leaseTTL = action.DefaultIdempotencyLeaseTTL
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.ensureOpenLocked(); err != nil {
		return err
	}
	claim, found := j.claims[key]
	if !found || claim.token != token || !time.Now().Before(claim.expiresAt) {
		return xerr.Conflict("file idempotency claim is missing or expired")
	}
	claim.expiresAt = time.Now().Add(leaseTTL)
	j.claims[key] = claim
	return nil
}

func (j *FileJournal) readIdempotencyLocked(ctx context.Context, key string) (fileIdempotencyRecord, bool, error) {
	if err := contextErr(ctx); err != nil {
		return fileIdempotencyRecord{}, false, err
	}
	data, err := j.root.ReadFile(fileKeyPath("idempotency", key))
	if errors.Is(err, fs.ErrNotExist) {
		return fileIdempotencyRecord{}, false, nil
	}
	if err != nil {
		return fileIdempotencyRecord{}, false, err
	}
	var record fileIdempotencyRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return fileIdempotencyRecord{}, false, fmt.Errorf("decode idempotency record: %w", err)
	}
	return record, true, nil
}

func (j *FileJournal) writeIdempotencyLocked(ctx context.Context, key string, record fileIdempotencyRecord) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode idempotency record: %w", err)
	}
	return j.root.WriteFileAtomic(fileKeyPath("idempotency", key), data, 0o600)
}

func (j *FileJournal) ensureOpenLocked() error {
	if j.closed {
		return xerr.Unavailable("file journal is closed")
	}
	return nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return xerr.BadRequest("file journal context is nil")
	}
	return ctx.Err()
}

func fileKeyPath(namespace string, keys ...string) string {
	h := sha256.New()
	for _, key := range keys {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(key)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(key))
	}
	return path.Join(namespace, hex.EncodeToString(h.Sum(nil))+".json")
}

func newFileJournalToken() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}
