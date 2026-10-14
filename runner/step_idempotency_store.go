package runner

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type scopedIdempotencyStore struct {
	inner  action.IdempotencyStore
	scope  string
	mu     sync.Mutex
	claims map[string]scopedIdempotencyClaim
}

type scopedIdempotencyClaim struct {
	token       string
	requestHash string
	expiresAt   time.Time
}

type renewingScopedIdempotencyStore struct {
	*scopedIdempotencyStore
}

var (
	_ action.IdempotencyCoordinator  = (*scopedIdempotencyStore)(nil)
	_ action.IdempotencyLeaseRenewer = (*renewingScopedIdempotencyStore)(nil)
)

func newScopedIdempotencyStore(inner action.IdempotencyStore, scope string) action.IdempotencyStore {
	store := &scopedIdempotencyStore{
		inner:  inner,
		scope:  scope,
		claims: make(map[string]scopedIdempotencyClaim),
	}
	_, coordinatesClaims := inner.(action.IdempotencyCoordinator)
	_, renewsClaims := inner.(action.IdempotencyLeaseRenewer)
	if coordinatesClaims && renewsClaims {
		return &renewingScopedIdempotencyStore{scopedIdempotencyStore: store}
	}
	return store
}

func (s *scopedIdempotencyStore) Get(ctx context.Context, key string) (action.IdempotencyEntry, bool) {
	return s.inner.Get(ctx, s.key(ctx, key))
}

func (s *scopedIdempotencyStore) Set(ctx context.Context, key string, entry action.IdempotencyEntry, ttl time.Duration) {
	s.inner.Set(ctx, s.key(ctx, key), entry, ttl)
}

func (s *scopedIdempotencyStore) Claim(ctx context.Context, key, requestHash string, leaseTTL time.Duration) (action.IdempotencyClaim, error) {
	if err := contextErr(ctx); err != nil {
		return action.IdempotencyClaim{}, err
	}
	namespaced := s.key(ctx, key)
	if coordinator, ok := s.inner.(action.IdempotencyCoordinator); ok {
		return coordinator.Claim(ctx, namespaced, requestHash, leaseTTL)
	}
	if leaseTTL <= 0 {
		leaseTTL = action.DefaultIdempotencyLeaseTTL
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, found := s.inner.Get(ctx, namespaced); found {
		if entry.RequestHash != "" && entry.RequestHash != requestHash {
			return action.IdempotencyClaim{State: action.IdempotencyClaimConflict}, nil
		}
		return action.IdempotencyClaim{State: action.IdempotencyClaimCompleted, Entry: entry}, nil
	}
	now := time.Now()
	if claim, exists := s.claims[namespaced]; exists {
		if now.Before(claim.expiresAt) {
			if claim.requestHash != requestHash {
				return action.IdempotencyClaim{State: action.IdempotencyClaimConflict}, nil
			}
			return action.IdempotencyClaim{State: action.IdempotencyClaimInProgress}, nil
		}
		delete(s.claims, namespaced)
	}
	token, err := newFileJournalToken()
	if err != nil {
		return action.IdempotencyClaim{}, xerr.Internal("create scoped idempotency claim", err)
	}
	s.claims[namespaced] = scopedIdempotencyClaim{
		token: token, requestHash: requestHash, expiresAt: now.Add(leaseTTL),
	}
	return action.IdempotencyClaim{State: action.IdempotencyClaimAcquired, Token: token}, nil
}

func (s *scopedIdempotencyStore) Complete(ctx context.Context, key, token string, entry action.IdempotencyEntry, ttl time.Duration) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	namespaced := s.key(ctx, key)
	if coordinator, ok := s.inner.(action.IdempotencyCoordinator); ok {
		return coordinator.Complete(ctx, namespaced, token, entry, ttl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	claim, found := s.claims[namespaced]
	if !found || claim.token != token || !time.Now().Before(claim.expiresAt) {
		return xerr.Conflict("scoped idempotency claim is missing or expired")
	}
	if entry.RequestHash == "" {
		entry.RequestHash = claim.requestHash
	}
	s.inner.Set(ctx, namespaced, entry, ttl)
	delete(s.claims, namespaced)
	return nil
}

func (s *scopedIdempotencyStore) Release(ctx context.Context, key, token string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	namespaced := s.key(ctx, key)
	if coordinator, ok := s.inner.(action.IdempotencyCoordinator); ok {
		return coordinator.Release(ctx, namespaced, token)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	claim, found := s.claims[namespaced]
	if !found {
		return nil
	}
	if claim.token != token {
		return xerr.Conflict("scoped idempotency claim token does not match")
	}
	delete(s.claims, namespaced)
	return nil
}

func (s *renewingScopedIdempotencyStore) RenewClaim(ctx context.Context, key, token string, leaseTTL time.Duration) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	namespaced := s.key(ctx, key)
	renewer, ok := s.inner.(action.IdempotencyLeaseRenewer)
	if !ok {
		return xerr.Internal("scoped idempotency store does not support lease renewal")
	}
	return renewer.RenewClaim(ctx, namespaced, token, leaseTTL)
}

func (s *scopedIdempotencyStore) key(ctx context.Context, key string) string {
	step, _ := stepIdentityContextKey.From(ctx)
	if step == "" {
		step = s.scope
	}
	h := sha256.New()
	for _, part := range []string{s.scope, step, key} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(part))
	}
	return "flow-" + hex.EncodeToString(h.Sum(nil))
}
