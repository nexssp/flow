package macros_test

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

// Every test asserts on two things: the actual file position of the
// failing token (clickable in modern terminals) and the macro context
// (name, definition line) that makes the error comprehensible without
// stepping through an expansion in a debugger.

func TestMacroError_SyntaxErrorInMultilineBody(t *testing.T) {
	// def line 2, body starts at line 3; the body-relative line 1 must
	// remap to file line 3.
	src := "\n" +
		"@macro broken() {\n" +
		"  noop -> )\n" +
		"}\n\n" +
		"@broken()\n"

	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected parse error")

	msg := err.Error()
	ktest.RequireStringContains(t, msg, "test.nflow:3:")
	ktest.RequireStringContains(t, msg, "in macro @broken")
	ktest.RequireStringContains(t, msg, "defined at test.nflow:2")
}

func TestMacroError_SyntaxErrorInInlineBody(t *testing.T) {
	// def line 2, body inline on the same line; body-relative line 1
	// remaps to file line 2.
	src := "\n" +
		"@macro short() { noop -> ) }\n" +
		"\n" +
		"@short()\n"

	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected parse error")

	msg := err.Error()
	ktest.RequireStringContains(t, msg, "test.nflow:2:")
	ktest.RequireStringContains(t, msg, "in macro @short")
}

func TestMacroError_SyntaxErrorDeepInBody(t *testing.T) {
	// def line 1, body starts at line 2. Body-relative line 4 remaps to
	// file line 5.
	src := "@macro deep() {\n" +
		"  const @{ value: \"a\" }\n" + // body line 1 → file line 2
		"  -> noop\n" + // body line 2 → file line 3
		"  -> noop\n" + // body line 3 → file line 4
		"  -> )\n" + // body line 4 → file line 5
		"}\n\n" +
		"@deep()\n"

	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected parse error")

	msg := err.Error()
	ktest.RequireStringContains(t, msg, "test.nflow:5:")
	ktest.RequireStringContains(t, msg, "in macro @deep")
	ktest.RequireStringContains(t, msg, "defined at test.nflow:1")
}

func TestMacroError_NestedMacroChain(t *testing.T) {
	// Inner's body has the failing token; outer wraps inner's error.
	// Both macro names and both definition lines must appear.
	src := "@macro inner() {\n" +
		"  noop -> )\n" +
		"}\n\n" +
		"@macro outer() {\n" +
		"  @inner()\n" +
		"}\n\n" +
		"@outer()\n"

	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected parse error")

	msg := err.Error()
	ktest.RequireStringContains(t, msg, "@inner")
	ktest.RequireStringContains(t, msg, "@outer")
	// The inner wrap remaps to the real file line of the failing
	// token inside inner's body: line 2.
	ktest.RequireStringContains(t, msg, "test.nflow:2:")
}

func TestMacroError_UnknownMacroReportsInvocation(t *testing.T) {
	// One declaration so the macros primary is installed. Without it,
	// @name falls through to the generic parser and produces
	// "unexpected token" rather than "unknown macro".
	src := "\n" +
		"@macro exists() {\n" +
		"  noop\n" +
		"}\n\n" +
		"@nonexistent()\n"

	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected unknown-macro error")

	msg := err.Error()
	ktest.RequireStringContains(t, msg, "test.nflow:6:")
	ktest.RequireStringContains(t, msg, "unknown macro")
	ktest.RequireStringContains(t, msg, "nonexistent")
}

func TestMacroError_UnclosedParenReportsPosition(t *testing.T) {
	src := "@macro ok() {\n  noop\n}\n\n@ok(noop"

	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected unclosed-paren error")

	msg := err.Error()
	ktest.RequireStringContains(t, msg, "test.nflow:5:")
	ktest.RequireStringContains(t, msg, "unclosed")
}

func TestMacroError_DefinitionAndActualPositionInOneMessage(t *testing.T) {
	// The DX contract: one clickable line that shows the failing token,
	// the macro's definition line, and the macro's name.
	src := "@macro broken() {\n" +
		"  noop -> )\n" +
		"}\n\n" +
		"@broken()\n"

	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected parse error")

	msg := err.Error()
	for _, want := range []string{
		"test.nflow:2:",           // failing token, clickable
		"in macro @broken",        // macro name
		"defined at test.nflow:1", // definition, clickable
	} {
		ktest.RequireCondition(t, strings.Contains(msg, want),
			"error must contain %q, got: %s", want, msg)
	}
}
