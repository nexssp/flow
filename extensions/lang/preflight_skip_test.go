package lang

import (
	"strings"
	"testing"
)

func TestReportSelfTestSkipReasonForMissingInterpreter(t *testing.T) {
	report := Report{Missing: []Check{{ID: "python", Version: "3", Tag: "py3", Err: ErrNotFound}}}
	if reason := report.SelfTestSkipReason(); !strings.Contains(reason, "lang.python@3") {
		t.Fatalf("SelfTestSkipReason() = %q; want a language-specific skip reason", reason)
	}
}

func TestReportSelfTestSkipReasonDoesNotHideUnknownLanguage(t *testing.T) {
	report := Report{Missing: []Check{{ID: "unknown", Err: &UnknownLanguageError{ID: "unknown"}}}}
	if reason := report.SelfTestSkipReason(); reason != "" {
		t.Fatalf("SelfTestSkipReason() = %q; unknown languages must fail", reason)
	}
}
