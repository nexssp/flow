package maputil

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestLookupPath(t *testing.T) {
	t.Parallel()
	root := map[string]any{"nested": map[string]any{"value": 42}, "scalar": "text"}
	for _, tc := range []struct {
		name   string
		path   string
		want   any
		wantOK bool
	}{
		{name: "empty path returns root", want: root, wantOK: true},
		{name: "nested value", path: "nested.value", want: 42, wantOK: true},
		{name: "missing key", path: "nested.missing"},
		{name: "non-map intermediate", path: "scalar.value"},
		{name: "trailing dot follows existing behavior", path: "nested.", want: map[string]any{"value": 42}, wantOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := LookupPath(root, tc.path)
			ktest.RequireEqual(t, ok, tc.wantOK)
			ktest.RequireEqual(t, got, tc.want)
		})
	}
}
