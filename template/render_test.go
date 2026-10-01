package template

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRender_InterpolatesAdjacentRepeatedAndNestedValues(t *testing.T) {
	t.Parallel()

	got, err := Render("{{.greeting}}{{.name}}/{{.name}} {{.profile.city}} {{.count}} {{.active}}", map[string]any{
		"greeting": "Hello, ",
		"name":     "Ada",
		"profile":  map[string]any{"city": "London"},
		"count":    0,
		"active":   false,
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if want := "Hello, Ada/Ada London 0 false"; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRender_InsertedValuesStayLiteral(t *testing.T) {
	t.Parallel()

	value := "{{.other}} $(touch never-created) `echo not-run` <b>raw</b>"
	got, err := Render("{{.value}}", map[string]any{
		"value": value,
		"other": "must not be substituted recursively",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got != value {
		t.Fatalf("Render() = %q, want inserted value unchanged %q", got, value)
	}
}

func TestRender_MissingVariableFailsWithoutPartialOutput(t *testing.T) {
	t.Parallel()

	var firstError string
	for attempt := range 2 {
		got, err := Render("prefix {{.missing}} suffix", map[string]any{"present": "value"})
		if got != "" {
			t.Fatalf("Render() returned partial output %q", got)
		}
		if !errors.Is(err, ErrExecute) {
			t.Fatalf("Render() error = %v, want ErrExecute", err)
		}
		if !strings.Contains(err.Error(), `missing`) {
			t.Fatalf("Render() error %q does not identify the missing key", err)
		}
		if attempt == 0 {
			firstError = err.Error()
		} else if err.Error() != firstError {
			t.Fatalf("missing-key errors differ: %q and %q", firstError, err)
		}
	}
}

func TestRender_ParseErrorIsClassified(t *testing.T) {
	t.Parallel()

	got, err := Render("{{.name", map[string]any{"name": "Ada"})
	if got != "" {
		t.Fatalf("Render() = %q, want empty output on parse error", got)
	}
	if !errors.Is(err, ErrParse) {
		t.Fatalf("Render() error = %v, want ErrParse", err)
	}
}

func TestRender_UnknownFunctionIsParseError(t *testing.T) {
	t.Parallel()

	_, err := Render(`{{unknownFunction .name}}`, map[string]any{"name": "Ada"})
	if !errors.Is(err, ErrParse) {
		t.Fatalf("Render() error = %v, want ErrParse", err)
	}
}

func TestRender_ExtraVariablesAreAllowed(t *testing.T) {
	t.Parallel()

	got, err := Render("{{.known}}", map[string]any{"known": "used", "unused": "ignored"})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got != "used" {
		t.Fatalf("Render() = %q, want %q", got, "used")
	}
}

func TestRender_EmptyTemplateAndNilVariables(t *testing.T) {
	t.Parallel()

	got, err := Render("", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got != "" {
		t.Fatalf("Render() = %q, want empty string", got)
	}
}

func ExampleRender() {
	got, err := Render("Hello {{.name}} — {{.literal}}", map[string]any{
		"name":    "srcpack",
		"literal": "$(this remains text)",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(got)
	// Output: Hello srcpack — $(this remains text)
}
