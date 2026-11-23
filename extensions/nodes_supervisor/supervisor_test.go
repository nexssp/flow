package nodes_supervisor

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"
)

func TestSupervisorRejectsTooManyChildTasks(t *testing.T) {
	tasks := make([]ChildTask, maxChildTasks+1)
	_, err := Supervisor.Do(t.Context(), SupervisorReq{Tasks: tasks})
	if got := xerr.KindFrom(err); got != xerr.KindBadRequest {
		t.Fatalf("task-count error kind = %s, want %s: %v", got, xerr.KindBadRequest, err)
	}
	want := "supervisor: 100001 child tasks exceeds limit of 100000"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("task-count error = %v, want message containing %q", err, want)
	}
}

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
