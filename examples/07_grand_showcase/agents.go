package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexssp/ai/guardrails"
	"github.com/nexssp/kernel/xerr"
)

type AgentRole string

const (
	RolePlanner     AgentRole = "master-planner"
	RoleArchitect   AgentRole = "code-architect"
	RoleSandbox     AgentRole = "sandbox-runner"
	RoleCloudSec    AgentRole = "cloud-security"
	RoleWasmSec     AgentRole = "wasm-security"
	RoleCriticJudge AgentRole = "eval-judge"
	RoleSynthesizer AgentRole = "deployer"
)

type LiveAgentHandle struct {
	ID        string
	SessionID string
	TenantID  string
	Role      AgentRole
	SpawnedAt time.Time
	onStop    func()
}

func (h *LiveAgentHandle) Stop(_ context.Context) {
	if h.onStop != nil {
		h.onStop()
	}
}

type AgentPool struct {
	mu           sync.RWMutex
	activeAgents map[string]*LiveAgentHandle
	totalSpawned atomic.Int64
	totalStopped atomic.Int64
}

func NewAgentPool() *AgentPool {
	return &AgentPool{
		activeAgents: make(map[string]*LiveAgentHandle),
	}
}

func (p *AgentPool) Spawn(_ context.Context, sessionID, tenantID string, role AgentRole) (*LiveAgentHandle, error) {
	id := fmt.Sprintf("%s-%s-%s-%d", sessionID, tenantID, role, time.Now().UnixNano())

	handle := &LiveAgentHandle{
		ID:        id,
		SessionID: sessionID,
		TenantID:  tenantID,
		Role:      role,
		SpawnedAt: time.Now().UTC(),
	}

	handle.onStop = func() {
		p.mu.Lock()
		delete(p.activeAgents, id)
		p.mu.Unlock()
		p.totalStopped.Add(1)
	}

	p.mu.Lock()
	p.activeAgents[id] = handle
	p.mu.Unlock()

	p.totalSpawned.Add(1)

	return handle, nil
}

func (p *AgentPool) Stats() (active int, spawned int64, stopped int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.activeAgents), p.totalSpawned.Load(), p.totalStopped.Load()
}

func InspectAndSanitize(prompt string) (string, bool, error) {
	if err := guardrails.CheckPromptInjection(prompt); err != nil {
		return "", false, xerr.Forbidden("FIREWALL: Prompt Injection / Jailbreak vector detected and neutralized!")
	}

	lowered := strings.ToLower(prompt)

	blockedKeywords := []string{"download malware", "reverse shell", "crypto miner", "sql injection payload"}
	for _, kw := range blockedKeywords {
		if strings.Contains(lowered, kw) {
			return "", false, xerr.Forbidden(fmt.Sprintf("FIREWALL: Prohibited security pattern detected: %q", kw))
		}
	}

	redacted := guardrails.RedactPII(prompt)
	wasRedacted := redacted != prompt

	return redacted, wasRedacted, nil
}
