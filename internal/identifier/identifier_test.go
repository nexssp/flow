package identifier

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestIdentifierByteClasses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		value        byte
		start        bool
		wantContinue bool
	}{
		{name: "underscore", value: '_', start: true, wantContinue: true},
		{name: "letter", value: 'z', start: true, wantContinue: true},
		{name: "digit", value: '3', wantContinue: true},
		{name: "hyphen", value: '-'},
		{name: "non-ascii byte", value: 0xE9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, IsStartByte(tc.value), tc.start)
			ktest.RequireEqual(t, IsContinueByte(tc.value), tc.wantContinue)
		})
	}
}

func TestIsValid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "letter", value: "value", want: true},
		{name: "underscore and digits", value: "_item2", want: true},
		{name: "empty", value: ""},
		{name: "digit start", value: "2item"},
		{name: "hyphen", value: "build-report"},
		{name: "punctuation", value: "item!"},
		{name: "non-ascii", value: "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, IsValid(tc.value), tc.want)
		})
	}
}

func TestIsQualified(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "qualified", value: "module.function", want: true},
		{name: "nested qualified", value: "module.sub.function", want: true},
		{name: "single part", value: "function"},
		{name: "empty part", value: "module..function"},
		{name: "invalid part", value: "module.function-name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, IsQualified(tc.value), tc.want)
		})
	}
}
