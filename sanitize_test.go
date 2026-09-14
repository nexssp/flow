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

func TestSanitizeDSL(t *testing.T) {
	src := "# comment\n" +
		"@config:budget_micros=1000\n" +
		"\n" +
		"agent.orchestrator:model=\"x\":role=\"Orchestrator\"@Break down task\n" +
		"  -> agent.worker:model=\"y\":skills=\"go\"@Implement\n" +
		"  -> sandbox.test_runner:timeout=30s\n" +
		"  -> agent.critic:model=\"x\"\n"

	want := `agent.orchestrator:model="x":role="Orchestrator" -> agent.worker:model="y":skills="go" -> sandbox.test_runner:timeout=30s -> agent.critic:model="x"`
	if got := SanitizeDSL(src); got != want {
		t.Errorf("\n got: %q\nwant: %q", got, want)
	}
}

func TestSanitizeDSL_SkipsRouteHeader(t *testing.T) {
	src := "autonomous.solve:route=\"POST /api/x\":status=200\nagent.planner -> agent.critic"

	want := "agent.planner -> agent.critic"
	if got := SanitizeDSL(src); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
