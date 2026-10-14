package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/nexssp/flow/runner"
)

func printApprovalSuspension(w io.Writer, sourceName string, options runner.DurabilityOptions, suspension *runner.ApprovalSuspension) {
	fmt.Fprintln(w, "\nApproval required; the flow is paused.")
	fmt.Fprintf(w, "  Gate:   %s\n", suspension.Gate)
	fmt.Fprintf(w, "  Role:   %s\n", suspension.Role)
	fmt.Fprintf(w, "  Reason: %s\n", suspension.Reason)
	fmt.Fprintf(w, "  Expires: %s\n", suspension.ExpiresAt.Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(w, "  Execution ID: %s\n", suspension.ExecutionID)
	if !suspension.Persisted {
		fmt.Fprintln(w, "  This pause was not persisted. Configure --journal-dir and start a new execution before relying on resume.")
		return
	}
	if options.JournalDir == "" {
		fmt.Fprintln(w, "  Resume with the same source, input, journal, and execution ID, plus --approval=<gate>.")
		return
	}
	if strings.HasPrefix(sourceName, "<") {
		fmt.Fprintln(w, "  Resume by rerunning the same inline source and input with:")
		fmt.Fprintf(w, "    --journal-dir %s --execution-id %s --approval=%s\n",
			shellQuote(options.JournalDir), shellQuote(suspension.ExecutionID), shellQuote(suspension.Gate))
		return
	}
	fmt.Fprintln(w, "  Resume with the same source and input using:")
	fmt.Fprintf(w, "    nflow run %s --journal-dir %s --execution-id %s --approval=%s\n",
		shellQuote(sourceName), shellQuote(options.JournalDir), shellQuote(suspension.ExecutionID), shellQuote(suspension.Gate))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
