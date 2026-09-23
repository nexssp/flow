package runner

import (
	"github.com/nexssp/flow/directives"
	"github.com/nexssp/kernel/action"
)

// MaterializeForTest exposes materializeFromPreprocessed to
// integration tests that live outside the runner package but need to
// verify that a domain materializer produces the expected actions.
//
// The "ForTest" suffix and the package doc make it clear this is not
// part of the public runtime API: it is not documented in user guides
// and its signature may change alongside the internal one.
func MaterializeForTest(pre *directives.Preprocessed, base *action.Registry) (*action.Registry, error) {
	return materializeFromPreprocessed(pre, base)
}
