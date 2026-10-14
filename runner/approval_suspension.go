package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	flowapproval "github.com/nexssp/flow/extensions/approval"
)

// ApprovalSuspension is the persisted description of a workflow paused at an
// approval gate. It stores hashes, not the source or input payload.
type ApprovalSuspension struct {
	ExecutionID string    `json:"execution_id"`
	Gate        string    `json:"gate"`
	Role        string    `json:"role"`
	Reason      string    `json:"reason"`
	SourceName  string    `json:"source_name"`
	SourceHash  string    `json:"source_hash"`
	InputHash   string    `json:"input_hash"`
	SuspendedAt time.Time `json:"suspended_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Persisted   bool      `json:"-"`
}

// SuspensionStore persists pending approval requests independently of completed
// durable steps. FileJournal implements this contract.
type SuspensionStore interface {
	LoadApprovalSuspension(ctx context.Context, executionID string) (ApprovalSuspension, bool, error)
	SaveApprovalSuspension(ctx context.Context, suspension ApprovalSuspension) error
	ClearApprovalSuspension(ctx context.Context, executionID string) error
}

type approvalFingerprint struct {
	sourceHash string
	inputHash  string
}

func sourceDigest(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func fingerprintApprovalRun(source, name string, payload any) (approvalFingerprint, error) {
	return fingerprintApprovalRunHash(sourceDigest(source), name, payload)
}

func fingerprintApprovalRunHash(sourceHash, _ string, payload any) (approvalFingerprint, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return approvalFingerprint{}, xerr.BadRequest("approval resume requires a JSON-compatible input payload", err)
	}
	inputSum := sha256.Sum256(encoded)
	return approvalFingerprint{
		sourceHash: sourceHash,
		inputHash:  hex.EncodeToString(inputSum[:]),
	}, nil
}

func runApprovalAware(
	ctx context.Context,
	journal any,
	executionID, sourceHash, name string,
	payload any,
	meta map[string]any,
	invoke func() (any, error),
) (any, *ApprovalSuspension, error) {
	declarations := flowapproval.DeclarationsFromMeta(meta)
	store, canPersist := journal.(SuspensionStore)
	var (
		fingerprint approvalFingerprint
		found       bool
		pending     ApprovalSuspension
	)
	if canPersist {
		var err error
		pending, found, err = store.LoadApprovalSuspension(ctx, executionID)
		if err != nil {
			return nil, nil, xerr.Unavailable("load pending approval suspension", err)
		}
	}
	if len(declarations) > 0 || found {
		var err error
		fingerprint, err = fingerprintApprovalRunHash(sourceHash, name, payload)
		if err != nil {
			return nil, nil, err
		}
	}
	if found {
		pending.Persisted = true
		if pending.SourceName != name || pending.SourceHash != fingerprint.sourceHash || pending.InputHash != fingerprint.inputHash {
			return nil, nil, xerr.Conflict("pending approval is bound to different source or input; use a new execution ID")
		}
		if !time.Now().Before(pending.ExpiresAt) {
			return nil, nil, xerr.Conflict("approval request expired; use a new execution ID")
		}
		if !flowapproval.TokenCovers(xctx.ApprovalTokenFrom(ctx), pending.Gate) {
			return nil, &pending, dag.Suspend(pending.Reason, pending.payload())
		}
	}

	output, runErr := invoke()
	if runErr != nil {
		return persistApprovalFailure(ctx, store, canPersist, executionID, sourceHash, name, payload, declarations, fingerprint, output, runErr)
	}
	if found {
		if err := store.ClearApprovalSuspension(ctx, executionID); err != nil {
			return output, nil, xerr.Unavailable("clear completed approval suspension", err)
		}
	}
	return output, nil, nil
}

func persistApprovalFailure(
	ctx context.Context,
	store SuspensionStore,
	canPersist bool,
	executionID, sourceHash, name string,
	payload any,
	declarations []flowapproval.Declaration,
	fingerprint approvalFingerprint,
	output any,
	runErr error,
) (any, *ApprovalSuspension, error) {
	suspendErr, ok := errors.AsType[*dag.SuspendError](runErr)
	if !ok {
		return output, nil, runErr
	}
	info, ok := approvalPayload(suspendErr.Payload, suspendErr.Reason, declarations)
	if !ok {
		return output, nil, runErr
	}
	suspension := ApprovalSuspension{
		ExecutionID: executionID,
		Gate:        info.gate,
		Role:        info.role,
		Reason:      suspendErr.Reason,
		SourceName:  name,
		SuspendedAt: time.Now().UTC(),
	}
	if fingerprint.sourceHash != "" {
		suspension.SourceHash = fingerprint.sourceHash
		suspension.InputHash = fingerprint.inputHash
	} else {
		computed, err := fingerprintApprovalRunHash(sourceHash, name, payload)
		if err != nil {
			return output, nil, err
		}
		suspension.SourceHash = computed.sourceHash
		suspension.InputHash = computed.inputHash
	}
	duration, err := time.ParseDuration(info.expires)
	if err != nil || duration <= 0 {
		return output, nil, xerr.Internal("approval gate supplied an invalid expiry")
	}
	suspension.ExpiresAt = suspension.SuspendedAt.Add(duration)
	if canPersist {
		if err := store.SaveApprovalSuspension(ctx, suspension); err != nil {
			return output, &suspension, xerr.Unavailable("persist approval suspension", err)
		}
		suspension.Persisted = true
	}
	return output, &suspension, runErr
}

type approvalPayloadDetails struct {
	gate    string
	role    string
	expires string
}

func approvalPayload(payload any, reason string, declarations []flowapproval.Declaration) (approvalPayloadDetails, bool) {
	values, ok := payload.(map[string]any)
	if !ok {
		return approvalPayloadDetails{}, false
	}
	gate, gateOK := values["gate"].(string)
	role, roleOK := values["role"].(string)
	payloadReason, reasonOK := values["reason"].(string)
	expires, expiresOK := values["expires"].(string)
	if !gateOK || !roleOK || !reasonOK || !expiresOK || gate == "" || role == "" || expires == "" {
		return approvalPayloadDetails{}, false
	}
	for _, declaration := range declarations {
		if declaration.Name == gate && declaration.Role == role && declaration.Reason == reason && payloadReason == reason && declaration.Expires.String() == expires {
			return approvalPayloadDetails{gate: gate, role: role, expires: expires}, true
		}
	}
	return approvalPayloadDetails{}, false
}

func (s ApprovalSuspension) payload() map[string]any {
	return map[string]any{
		"gate":    s.Gate,
		"role":    s.Role,
		"reason":  s.Reason,
		"expires": s.ExpiresAt.Sub(s.SuspendedAt).String(),
	}
}
