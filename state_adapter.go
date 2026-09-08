package flow

import (
	"github.com/nexssp/kernel/ai/dag"
)

// AcquireStateFromGraphState converts a graph.State into a pooled dag.State without manual map copying.
func AcquireStateFromGraphState(s *State) *dag.State {
	target := dag.AcquireState()
	if s == nil || s.data == nil {
		return target
	}
	for k, v := range s.data {
		target.Set(k, v)
	}
	return target
}
