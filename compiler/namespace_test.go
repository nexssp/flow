package compiler

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestIsValidNamespaceQualifier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "ascii", value: "shared", want: true},
		{name: "unicode letters", value: "变量", want: true},
		{name: "underscore", value: "_shared", want: true},
		{name: "digit suffix", value: "shared2", want: true},
		{name: "empty"},
		{name: "blank identifier", value: "_"},
		{name: "keyword", value: "for"},
		{name: "digit prefix", value: "2shared"},
		{name: "hyphen", value: "shared-name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, IsValidNamespaceQualifier(tc.value), tc.want)
		})
	}
}
