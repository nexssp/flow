package projection

import (
	"fmt"
	"strings"
)

func projectionEvaluationMessage(raw string) string {
	return fmt.Sprintf(
		"projection %q: evaluation failed. If an input field and pipeline `as` binding share a name, use .field or input.field to select the input explicitly, or rename the binding to select its output.",
		strings.TrimSpace(raw),
	)
}
