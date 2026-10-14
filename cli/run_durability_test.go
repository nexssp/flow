package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSourceInProcess_ResumesFromDurableJournal(t *testing.T) {
	journalDir := filepath.Join(t.TempDir(), "journal")
	const executionID = "cli-resume-test-2026-10-14"
	args := []string{"--journal-dir", journalDir, "--execution-id", executionID, "--json"}

	stdout, stderr := captureIO(t, func() {
		if code := runSourceInProcess(t.Context(), `const @{ value: "saved" }`, "resume.nflow", args); code != 0 {
			t.Fatalf("first run returned %d", code)
		}
	})
	if strings.TrimSpace(stdout) != "saved" {
		t.Fatalf("first stdout = %q, want saved result", stdout)
	}
	if !strings.Contains(stderr, "Execution ID: "+executionID) {
		t.Fatalf("first stderr did not report execution ID %q:\n%s", executionID, stderr)
	}

	stdout, stderr = captureIO(t, func() {
		if code := runSourceInProcess(t.Context(), `const @{ value: "edited" }`, "resume.nflow", args); code != 0 {
			t.Fatalf("resume returned %d", code)
		}
	})
	if strings.TrimSpace(stdout) != "saved" {
		t.Fatalf("resumed stdout = %q, want the journaled result", stdout)
	}
	if !strings.Contains(stderr, "Execution ID: "+executionID) {
		t.Fatalf("resumed stderr did not report execution ID %q:\n%s", executionID, stderr)
	}
}

func TestRunSourceInProcess_ExecutionIDRequiresJournal(t *testing.T) {
	_, stderr := captureIO(t, func() {
		if code := runSourceInProcess(t.Context(), `const @{ value: "value" }`, "resume.nflow", []string{"--execution-id=run-1"}); code != 2 {
			t.Fatalf("run returned %d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "--execution-id requires --journal-dir") {
		t.Fatalf("stderr = %q, want missing journal diagnostic", stderr)
	}
}

func TestSplitArgs_DurabilityValuesBeforeTarget(t *testing.T) {
	target, _, flags := splitArgs([]string{
		"--journal-dir", ".flow-state",
		"--execution-id", "run-42",
		"flows/invoice.nflow",
		"--json",
	})
	if target != "flows/invoice.nflow" {
		t.Fatalf("target = %q, want flow path", target)
	}
	if got := strings.Join(flags, " "); got != "--journal-dir=.flow-state --execution-id=run-42 --json" {
		t.Fatalf("flags = %q, want normalized durability flags", got)
	}
}
