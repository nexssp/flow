// nexssp/flow/compiler/parser_test.go
package compiler_test

import (
	"bufio"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestParser_AllLanguageCommands(t *testing.T) {
	var dslCommands = `
# 1. Simple Action
github.issue

# 2. Sequential Pipe
github.issue -> ai.triage -> slack.post

# 3. Parallel Scatter-Gather
pack -> (sec.audit & arch.review) -> review.gate

# 4. Fallback Chain
redis.get || postgres.query || legacy.rest_api

# 5. Conditional Branch
check_tests ? k8s.deploy

# 6. Full Conditional with Pipe (Condition binds loosely)
git.pull -> make.build -> go.test -> check_tests ? k8s.deploy

# 7. Inline Data Projections
github.issue -> { prompt: "Triage: " + issue.title } -> ai.triage

# 8. Action with Modifiers (Profile, Target, Excludes, Prompt)
srcpack.pack:arch#pkg~testdata@security

# 9. Action with Parameters (Handling hyphens in identifiers and negative numbers)
security.audit(code=pack.content, env="staging", retries=-1, my-param=1, hyphen-arg="val")

# 10. Autonomous Loop
agent.planner -> loop( agent.reason || tool.web_search ) until( completed == true ) -> agent.summarize

# 11. Complex Saga with Rollbacks
( flight.book(rollback=flight.cancel) -> hotel.reserve(rollback=hotel.release) ) & ( inventory.lock(rollback=inventory.unlock) )

# 12. Mix of everything
ingress.auth:retry=2 -> { tx: tx_id } -> ( edge.eu-central-1 & edge.us-east-1 ) -> mesh.aggregate || alert.pagerduty
`
	scanner := bufio.NewScanner(strings.NewReader(dslCommands))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parser := compiler.NewParser(line)
		_, err := parser.ParseExpression()
		if err != nil {
			t.Errorf("Failed to parse DSL command:\n   [ %s ]\nError: %v", line, err)
		}
	}
}

func TestParser(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
		check   func(t *testing.T, expr compiler.Expr)
	}{
		{
			name:  "simple atom",
			input: "github.issue",
			check: func(t *testing.T, e compiler.Expr) {
				atom, ok := e.(*compiler.AtomExpr)
				if !ok {
					t.Fatalf("expected AtomExpr, got %T", e)
				}
				if atom.Name != "github.issue" {
					t.Errorf("name = %q, want github.issue", atom.Name)
				}
			},
		},
		{
			name:  "atom with modifiers",
			input: "srcpack.pack:arch#pkg~testdata@security",
			check: func(t *testing.T, e compiler.Expr) {
				atom := e.(*compiler.AtomExpr)
				if atom.Profile != "arch" {
					t.Errorf("profile = %q, want arch", atom.Profile)
				}
				if atom.Prompt != "security" {
					t.Errorf("prompt = %q, want security", atom.Prompt)
				}
				if len(atom.Targets) != 1 || atom.Targets[0] != "pkg" {
					t.Errorf("targets = %v, want [pkg]", atom.Targets)
				}
				if len(atom.Excludes) != 1 || atom.Excludes[0] != "testdata" {
					t.Errorf("excludes = %v, want [testdata]", atom.Excludes)
				}
			},
		},
		{
			name:  "atom with params including negative numbers and hyphens",
			input: "security.audit(code=pack.content, env=\"staging\", retry=-1, my-param=true)",
			check: func(t *testing.T, e compiler.Expr) {
				atom := e.(*compiler.AtomExpr)
				if len(atom.Inputs) != 1 || atom.Inputs["code"] != "pack.content" {
					t.Errorf("inputs = %v, want map[code:pack.content]", atom.Inputs)
				}
				if atom.Params["env"] != "staging" {
					t.Errorf("params[env] = %v, want staging", atom.Params["env"])
				}
				if atom.Params["retry"] != "-1" {
					t.Errorf("params[retry] = %v, want -1", atom.Params["retry"])
				}
				if atom.Params["my-param"] != "true" {
					t.Errorf("params[my-param] = %v, want true", atom.Params["my-param"])
				}
			},
		},
		{
			name:  "pipeline: A -> B",
			input: "A -> B",
			check: func(t *testing.T, e compiler.Expr) {
				pipe, ok := e.(*compiler.PipelineExpr)
				if !ok {
					t.Fatalf("expected PipelineExpr, got %T", e)
				}
				_, leftOK := pipe.Left.(*compiler.AtomExpr)
				_, rightOK := pipe.Right.(*compiler.AtomExpr)
				if !leftOK || !rightOK {
					t.Error("left or right not AtomExpr")
				}
			},
		},
		{
			name:  "parallel: A & B",
			input: "A & B",
			check: func(t *testing.T, e compiler.Expr) {
				par, ok := e.(*compiler.ParallelExpr)
				if !ok {
					t.Fatalf("expected ParallelExpr, got %T", e)
				}
				if len(par.Children) != 2 {
					t.Errorf("children count = %d, want 2", len(par.Children))
				}
			},
		},
		{
			name:  "fallback: A || B",
			input: "A || B",
			check: func(t *testing.T, e compiler.Expr) {
				fall, ok := e.(*compiler.FallbackExpr)
				if !ok {
					t.Fatalf("expected FallbackExpr, got %T", e)
				}
				if fall.Left == nil || fall.Right == nil {
					t.Error("fallback left or right is nil")
				}
			},
		},
		{
			name:  "conditional: A ? B",
			input: "A ? B",
			check: func(t *testing.T, e compiler.Expr) {
				cond, ok := e.(*compiler.ConditionalExpr)
				if !ok {
					t.Fatalf("expected ConditionalExpr, got %T", e)
				}
				if cond.Gate == nil || cond.Target == nil {
					t.Error("conditional gate or target is nil")
				}
			},
		},
		{
			name:  "loop autonomous logic",
			input: "loop( A || B ) until( done == true )",
			check: func(t *testing.T, e compiler.Expr) {
				loopExpr, ok := e.(*compiler.LoopExpr)
				if !ok {
					t.Fatalf("expected LoopExpr, got %T", e)
				}
				if loopExpr.Until != "done == true" {
					t.Errorf("until condition = %q, want 'done == true'", loopExpr.Until)
				}
			},
		},
		{
			name:  "projection: { prompt: \"hello\" + name }",
			input: "{ prompt: \"hello\" + name }",
			check: func(t *testing.T, e compiler.Expr) {
				proj, ok := e.(*compiler.ProjectionExpr)
				if !ok {
					t.Fatalf("expected ProjectionExpr, got %T", e)
				}
				expected := `prompt: "hello" + name`
				if proj.Raw != expected {
					t.Errorf("raw = %q, want %q", proj.Raw, expected)
				}
			},
		},
		{
			name:    "error: missing closing paren",
			input:   "(A & B -> C",
			wantErr: true,
			errMsg:  "expected ')'",
		},
		{
			name:    "error: invalid character",
			input:   "A $ B",
			wantErr: true,
			errMsg:  "invalid character",
		},
		{
			name:    "error: unexpected token after expression",
			input:   "A B",
			wantErr: true,
			errMsg:  "unexpected token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := compiler.NewParser(tt.input)
			expr, err := parser.ParseExpression()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, expr)
			}
		})
	}
}
