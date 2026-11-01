package macros

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestTypedMacroArgumentKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		kind    string
		arg     string
		valid   bool
		wantErr string
	}{
		{name: "Identifier accepts bare name", kind: "Identifier", arg: "field_2", valid: true},
		{name: "Identifier rejects qualified name", kind: "Identifier", arg: "field.name", wantErr: "bare Identifier"},
		{name: "String accepts quoted text", kind: "String", arg: `"report"`, valid: true},
		{name: "String rejects bare text", kind: "String", arg: "report", wantErr: "quoted String"},
		{name: "String rejects unterminated quote", kind: "String", arg: `"report`, wantErr: "quoted String"},
		{name: "Integer accepts signed integer", kind: "Integer", arg: "-42", valid: true},
		{name: "Integer rejects float", kind: "Integer", arg: "42.0", wantErr: "Integer literal"},
		{name: "Integer rejects quoted digits", kind: "Integer", arg: `"42"`, wantErr: "Integer literal"},
		{name: "Expr accepts Flow action expression", kind: "Expr", arg: "runtime.noop", valid: true},
		{name: "Expr rejects incomplete expression", kind: "Expr", arg: "runtime.noop ->", wantErr: "valid Flow Expr"},
		{name: "ActionRef accepts capability syntax", kind: "ActionRef", arg: "runtime.noop", valid: true},
		{name: "ActionRef accepts unregistered syntax", kind: "ActionRef", arg: "unregistered.capability", valid: true},
		{name: "ActionRef rejects quoted text", kind: "ActionRef", arg: `"runtime.noop"`, wantErr: "ActionRef capability name"},
		{name: "ActionRef rejects call syntax", kind: "ActionRef", arg: "runtime.noop()", wantErr: "ActionRef capability name"},
		{name: "ActionRef rejects literal", kind: "ActionRef", arg: "true", wantErr: "ActionRef capability name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			declaration := Declaration{
				Name:       "typed",
				Params:     []string{"value"},
				ParamKinds: []string{tc.kind},
			}
			err := validateTypedArguments(newTestParser(t, nil, "runtime.noop"), "typed", 1, declaration, []string{tc.arg})
			if tc.valid {
				ktest.RequireNoError(t, err)
				return
			}
			ktest.RequireErrorContains(t, err, tc.wantErr)
		})
	}
}
