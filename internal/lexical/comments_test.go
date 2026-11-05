package lexical

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestIsHeaderComment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{name: "hash comment", text: "# heading", want: true},
		{name: "slash comment", text: "// heading", want: true},
		{name: "empty"},
		{name: "leading space", text: " # heading"},
		{name: "single slash", text: "/ heading"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, IsHeaderComment(tc.text), tc.want)
		})
	}
}
