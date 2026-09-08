package flow

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/flow/journal"
)

func (g *CompiledGraph) SelectOutgoingDurable(ctx context.Context, j journal.BranchJournal, runID, source string, state *State) ([]CompiledEdge, error) {
	if j == nil {
		return nil, fmt.Errorf("graph: branch journal is required")
	}
	if runID == "" {
		return nil, fmt.Errorf("graph: run ID is required")
	}

	edges := g.Outgoing[source]
	if len(edges) == 0 {
		return nil, nil
	}

	stored, found, err := j.Get(ctx, runID, source)
	if err != nil {
		return nil, fmt.Errorf("graph: read branch decision: %w", err)
	}
	if found {
		return replaySelectedEdges(edges, stored)
	}

	selected, err := g.SelectOutgoing(source, func(condition string) (bool, error) {
		return EvaluateCondition(condition, state)
	})
	if err != nil {
		return nil, err
	}

	selectedKeys := make(map[string]bool, len(selected))
	for _, edge := range selected {
		selectedKeys[edge.From+"\x00"+edge.To] = true
	}

	now := time.Now().UTC()
	records := make([]journal.BranchRecord, 0, len(edges))

	for i, edge := range edges {
		status := journal.BranchSkipped
		reason := "condition_not_matched"

		if selectedKeys[edge.From+"\x00"+edge.To] {
			status = journal.BranchSelected
			reason = "condition_matched"
		}
		if edge.Otherwise {
			reason = "fallback"
		}

		records = append(records, journal.BranchRecord{
			RunID:       runID,
			SourceNode:  source,
			From:        edge.From,
			To:          edge.To,
			When:        edge.When,
			Otherwise:   edge.Otherwise,
			Status:      status,
			Reason:      reason,
			DecisionSeq: i,
			CreatedAt:   now,
		})
	}

	if putErr := j.Put(ctx, runID, source, records); putErr != nil {
		return nil, fmt.Errorf("graph: persist branch decision: %w", putErr)
	}

	return selected, nil
}

func replaySelectedEdges(edges []CompiledEdge, records []journal.BranchRecord) ([]CompiledEdge, error) {
	known := make(map[string]CompiledEdge, len(edges))
	for _, edge := range edges {
		known[edge.From+"\x00"+edge.To] = edge
	}

	if len(records) != len(edges) {
		return nil, fmt.Errorf("graph: persisted decision has %d records, graph has %d edges", len(records), len(edges))
	}

	selected := make(map[string]bool, len(records))

	for i := range records {
		record := &records[i]
		key := record.From + "\x00" + record.To

		edge, ok := known[key]
		if !ok {
			return nil, fmt.Errorf("graph: unknown persisted branch edge %s -> %s", record.From, record.To)
		}

		if edge.When != record.When || edge.Otherwise != record.Otherwise {
			return nil, fmt.Errorf("graph: persisted metadata differs for %s -> %s", record.From, record.To)
		}

		if _, dup := selected[key]; dup {
			return nil, fmt.Errorf("graph: duplicate persisted branch edge %s -> %s", record.From, record.To)
		}

		if record.Status != journal.BranchSelected && record.Status != journal.BranchSkipped {
			return nil, fmt.Errorf("graph: invalid persisted branch status %q", record.Status)
		}

		selected[key] = record.Status == journal.BranchSelected
	}

	result := make([]CompiledEdge, 0)
	for _, edge := range edges {
		if selected[edge.From+"\x00"+edge.To] {
			result = append(result, edge)
		}
	}
	return result, nil
}
