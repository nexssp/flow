package runner

import (
	"strings"
	"testing"
)

func TestParseDurabilityOptions(t *testing.T) {
	got, remaining, err := ParseDurabilityOptions([]string{
		"flow.nflow",
		"--journal-dir", ".flow-state",
		"--json",
		"--execution-id=invoice-42",
	})
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	if got.JournalDir != ".flow-state" || got.ExecutionID != "invoice-42" {
		t.Fatalf("options = %#v", got)
	}
	if strings.Join(remaining, " ") != "flow.nflow --json" {
		t.Fatalf("remaining = %#v", remaining)
	}
}

func TestParseDurabilityOptionsRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing journal directory", args: []string{"--journal-dir"}, want: "requires a value"},
		{name: "empty journal directory", args: []string{"--journal-dir="}, want: "cannot be empty"},
		{name: "missing execution ID", args: []string{"--execution-id", "--journal-dir=state"}, want: "requires a value"},
		{name: "duplicate journal directory", args: []string{"--journal-dir=one", "--journal-dir=two"}, want: "only once"},
		{name: "duplicate execution ID", args: []string{"--execution-id=one", "--execution-id=two"}, want: "only once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseDurabilityOptions(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
