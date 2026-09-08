package flow

import (
	"context"
	"fmt"
	"sort"
)

type SpawnedNode struct {
	RunID      string       `json:"run_id"`
	SourceNode string       `json:"source_node"`
	Edge       CompiledEdge `json:"edge"`
	TargetNode string       `json:"target_node"`
	Input      *State       `json:"input"`
	SpawnIndex int          `json:"spawn_index"`
}

type NodeResult[Res any] struct {
	Spawned SpawnedNode `json:"spawned"`
	Value   Res         `json:"value"`
	Err     error       `json:"error,omitempty"`
	Usage   CostUsage   `json:"usage"`
}

type FanInPolicy struct {
	RequireAll   bool
	AllowPartial bool
	FailOnEmpty  bool
}

type RecoveryStrategy string

const (
	RecoveryFailFast        RecoveryStrategy = "fail_fast"
	RecoveryRetryFailed     RecoveryStrategy = "retry_failed"
	RecoveryContinuePartial RecoveryStrategy = "continue_partial"
)

type RecoveryPolicy struct {
	Strategy           RecoveryStrategy `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	MaxAttempts        int              `json:"max_attempts,omitempty" yaml:"max_attempts,omitempty"`
	BackoffMS          int64            `json:"backoff_ms,omitempty" yaml:"backoff_ms,omitempty"`
	MaxBackoffMS       int64            `json:"max_backoff_ms,omitempty" yaml:"max_backoff_ms,omitempty"`
	RetryTransientOnly bool             `json:"retry_transient_only,omitempty" yaml:"retry_transient_only,omitempty"`
}

// SpawnSelected creates durable invocation units for each selected edge.
func SpawnSelected(runID, source string, selected []CompiledEdge, input *State) ([]SpawnedNode, error) {
	if runID == "" || source == "" {
		return nil, fmt.Errorf("graph: run ID and source node are required")
	}
	if len(selected) == 0 {
		return nil, nil
	}

	out := make([]SpawnedNode, len(selected))
	for i, edge := range selected {
		out[i] = SpawnedNode{
			RunID:      runID,
			SourceNode: source,
			Edge:       edge,
			TargetNode: edge.To,
			Input:      input,
			SpawnIndex: i,
		}
	}
	return out, nil
}

// FanIn deterministically orders parallel child outputs by SpawnIndex and calls reduce.
func FanIn[Res any](
	ctx context.Context,
	input *State,
	results []NodeResult[Res],
	policy FanInPolicy,
	reduce func(context.Context, *State, []NodeResult[Res]) (*State, error),
) (*State, error) {
	if reduce == nil {
		return nil, fmt.Errorf("graph: fan-in reducer is required")
	}

	ordered := append([]NodeResult[Res](nil), results...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Spawned.SpawnIndex < ordered[j].Spawned.SpawnIndex
	})

	if policy.RequireAll {
		for _, result := range ordered {
			if result.Err != nil {
				return nil, fmt.Errorf("graph: required child %s failed: %w", result.Spawned.TargetNode, result.Err)
			}
		}
	} else if !policy.AllowPartial {
		for _, result := range ordered {
			if result.Err != nil {
				return nil, fmt.Errorf("graph: child %s failed: %w", result.Spawned.TargetNode, result.Err)
			}
		}
	}

	if policy.FailOnEmpty && len(ordered) == 0 {
		return nil, fmt.Errorf("graph: fan-in received no child results")
	}

	return reduce(ctx, input, ordered)
}
