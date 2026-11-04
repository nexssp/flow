package nodes_supervisor

import (
	"testing"

	"github.com/nexssp/kernel/xerr"
)

func TestSupervisorRejectsInvalidTimeoutMS(t *testing.T) {
	for _, timeout := range []int64{-1, maxChildTimeoutMS + 1} {
		_, err := Supervisor.Do(t.Context(), SupervisorReq{
			Tasks: []ChildTask{{ID: "invalid-timeout", TimeoutMS: timeout}},
		})
		if got := xerr.KindFrom(err); got != xerr.KindBadRequest {
			t.Fatalf("timeout_ms %d error kind = %s, want %s: %v", timeout, got, xerr.KindBadRequest, err)
		}
	}
}
