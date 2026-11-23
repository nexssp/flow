package runner

import "testing"

func TestAppendObserverHistoryHandlesNonPositiveCapacity(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		history := []string{"existing"}
		got, dropped := appendObserverHistory(history, "next", capacity)
		if !dropped {
			t.Errorf("capacity %d: dropped = false, want true", capacity)
		}
		if len(got) != 1 || got[0] != "existing" {
			t.Errorf("capacity %d: history = %#v, want the original entry", capacity, got)
		}
	}
}
