package core

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestModifiersToMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want map[string]any
	}{
		{
			name: "empty input returns nil",
			in:   nil,
			want: nil,
		},
		{
			name: "bare flag becomes true",
			in:   []string{"quiet"},
			want: map[string]any{"quiet": true},
		},
		{
			name: "key equals value stays string",
			in:   []string{"ext=go"},
			want: map[string]any{"ext": "go"},
		},
		{
			name: "bool-like strings stay strings",
			in:   []string{"active=true"},
			want: map[string]any{"active": "true"},
		},
		{
			name: "false-like strings stay strings",
			in:   []string{"active=false"},
			want: map[string]any{"active": "false"},
		},
		{
			name: "quoted value strips quotes",
			in:   []string{`name="quoted"`},
			want: map[string]any{"name": "quoted"},
		},
		{
			name: "keys lowercased",
			in:   []string{"EDITOR=zed"},
			want: map[string]any{"editor": "zed"},
		},
		{
			name: "multiple modifiers preserve last for duplicate key",
			in:   []string{"a=1", "b=2", "flag"},
			want: map[string]any{"a": "1", "b": "2", "flag": true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, ModifiersToMap(test.in), test.want)
		})
	}
}
