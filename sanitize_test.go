package flow

import "testing"

func TestStripAtAnnotations(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no annotation", `agent.planner:model="x"`, `agent.planner:model="x"`},
		{"trailing annotation", `agent.planner:role=admin@Do X`, `agent.planner:role=admin`},
		{"quoted at preserved", `agent.planner:payload="a@b"@Do X`, `agent.planner:payload="a@b"`},
		{"single-quoted at", `agent.planner:payload='a@b'@Do X`, `agent.planner:payload='a@b'`},
		{"backtick quoted at", "agent.planner:payload=`a@b`@Do X", "agent.planner:payload=`a@b`"},
		{"escaped quote inside", `agent.planner:x="a\"@b"@Do X`, `agent.planner:x="a\"@b"`},
		{"annotation only", `@Do X`, ``},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripAtAnnotations(c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestSanitizeDSL verifies the two behaviours SanitizeDSL guarantees:
//
//  1. Structural comments and directives are replaced by empty lines,
//     so line numbers in the output match the input.
//  2. Executable lines are preserved verbatim, including modifiers
//     and @-prompts.
func TestSanitizeDSL(t *testing.T) {
	input := "# System Runtime Configurations\n" +
		"@config:budget_micros=10000000\n" +
		"\n" +
		"# Swarm Pipeline\n" +
		"agent.orchestrator:model=\"x\":role=\"Orchestrator\"@Break down task\n" +
		"-> agent.worker:model=\"y\":skills=\"go\"@Implement\n" +
		"-> sandbox.test_runner:timeout=30s\n" +
		"-> agent.critic:model=\"x\"\n"

	want := "\n" +
		"\n" +
		"\n" +
		"\n" +
		"agent.orchestrator:model=\"x\":role=\"Orchestrator\"@Break down task\n" +
		"-> agent.worker:model=\"y\":skills=\"go\"@Implement\n" +
		"-> sandbox.test_runner:timeout=30s\n" +
		"-> agent.critic:model=\"x\"\n"

	got := SanitizeDSL(input)
	if got != want {
		t.Fatalf("SanitizeDSL:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestSanitizeDSL_SkipsRouteHeader verifies that an unindented
// pipeline header line carrying :route= or :http= is treated as a
// route declaration and stripped.
func TestSanitizeDSL_SkipsRouteHeader(t *testing.T) {
	input := "autonomous.solve:route=\"POST /api/x\":status=200\n" +
		"agent.planner -> agent.critic\n"

	want := "\n" +
		"agent.planner -> agent.critic\n"

	got := SanitizeDSL(input)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestSanitizeDSL_PreservesLineCount pins the invariant Preprocess
// relies on: output line count equals input line count.
func TestSanitizeDSL_PreservesLineCount(t *testing.T) {
	input := "@a\n@b\n\n# c\nx\n-> y\n"
	got := SanitizeDSL(input)

	inLines := 0
	for _, c := range input {
		if c == '\n' {
			inLines++
		}
	}
	outLines := 0
	for _, c := range got {
		if c == '\n' {
			outLines++
		}
	}
	if inLines != outLines {
		t.Fatalf("line count changed: %d -> %d", inLines, outLines)
	}
}
