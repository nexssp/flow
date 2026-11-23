package realtime

import (
	"testing"
	"time"
)

func TestDurationFromMillisecondsBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value int64
		valid bool
	}{
		{name: "zero", value: 0, valid: true},
		{name: "maximum", value: maxDurationMilliseconds, valid: true},
		{name: "minimum", value: -maxDurationMilliseconds, valid: true},
		{name: "overflow above", value: maxDurationMilliseconds + 1},
		{name: "overflow below", value: -maxDurationMilliseconds - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := durationFromMilliseconds(tc.value)
			if ok != tc.valid {
				t.Fatalf("durationFromMilliseconds(%d) valid = %v, want %v", tc.value, ok, tc.valid)
			}
			if ok && got != time.Duration(tc.value)*time.Millisecond {
				t.Fatalf("durationFromMilliseconds(%d) = %s, want %s", tc.value, got, time.Duration(tc.value)*time.Millisecond)
			}
		})
	}
}
