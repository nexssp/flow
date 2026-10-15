package retry

import (
	"testing"

	"github.com/nexssp/kernel/xerr"
)

func TestConfigFromOptionsErrorsUseValidationKind(t *testing.T) {
	t.Parallel()
	if _, err := ConfigFromOptions(map[string]string{"default_max": "not-a-number"}); xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("invalid retry options error = %v, want Validation", err)
	}
	if _, err := ConfigFromOptions(map[string]string{"backoff": "unsupported"}); xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("unsupported backoff error = %v, want Validation", err)
	}
}

func TestConfigValidateErrorsUseValidationKind(t *testing.T) {
	t.Parallel()
	cfg := Config{
		DefaultMax: -1,
		Backoff:    BackoffConstant,
		MinBackoff: 1,
		MaxBackoff: 1,
	}
	if err := cfg.Validate(); xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("invalid retry configuration error = %v, want Validation", err)
	}
}
