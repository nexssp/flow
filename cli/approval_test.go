package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/runner"
)

func TestRunSourceInProcess_PersistsAndResumesApproval(t *testing.T) {
	journalDir := filepath.Join(t.TempDir(), "journal")
	const executionID = "cli-approval-resume-1"
	source := `@approval finance_review {
  role: finance reviewer
  reason: confirm the transfer
  expires: 1h
}
const @{ value: "approved" } -> approval.finance_review`
	args := []string{"--journal-dir", journalDir, "--execution-id", executionID, "--json"}
	stdout, stderr := captureIO(t, func() {
		if code := runSourceInProcess(t.Context(), source, "approval-cli.nflow", args); code != 3 {
			t.Fatalf("pause returned %d, want 3", code)
		}
	})
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("paused stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"Approval required", "finance_review", "Execution ID: " + executionID, "--approval="} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("pause stderr missing %q:\n%s", want, stderr)
		}
	}

	resumeArgs := append(append([]string(nil), args...), "--approval=finance_review")
	stdout, stderr = captureIO(t, func() {
		if code := runSourceInProcess(t.Context(), source, "approval-cli.nflow", resumeArgs); code != 0 {
			t.Fatalf("approved resume returned %d:\n%s", code, stderr)
		}
	})
	if strings.TrimSpace(stdout) != "approved" {
		t.Fatalf("resumed stdout = %q, want approved output", stdout)
	}
}

func TestParseApprovalOption(t *testing.T) {
	for _, args := range [][]string{
		{"--approval=review", "--json"},
		{"--approval", "review", "--json"},
	} {
		token, remaining, err := runner.ParseApprovalOption(args)
		if err != nil {
			t.Fatal(err)
		}
		if token != "review" || strings.Join(remaining, " ") != "--json" {
			t.Fatalf("ParseApprovalOption(%q) = %q, %q", args, token, remaining)
		}
	}
}
