package compiler_test

import (
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

// Helpers for asserting on typed Values without repeating the kind checks.
func argStr(t *testing.T, v *compiler.Value) string {
	t.Helper()
	if v == nil {
		t.Fatal("arg is nil")
	}
	if v.Kind != compiler.ValueString {
		t.Fatalf("arg kind = %v, want string", v.Kind)
	}
	return v.Str
}

func argRef(t *testing.T, v *compiler.Value) string {
	t.Helper()
	if v == nil {
		t.Fatal("arg is nil")
	}
	if v.Kind != compiler.ValueRef {
		t.Fatalf("arg kind = %v, want ref", v.Kind)
	}
	return v.Ref
}

// ─── ternary with else ─────────────────────────────────────────────────

func TestConditional_TwoArm(t *testing.T) {
	expr, err := compiler.NewParser(`approved ? repo.merge`).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := expr.(*compiler.ConditionalExpr)
	if !ok {
		t.Fatalf("got %T, want ConditionalExpr", expr)
	}
	if c.Else != nil {
		t.Errorf("Else should be nil in two-arm form")
	}
}

func TestConditional_ThreeArm(t *testing.T) {
	expr, err := compiler.NewParser(`approved ? repo.merge : human.review`).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := expr.(*compiler.ConditionalExpr)
	if !ok {
		t.Fatalf("got %T, want ConditionalExpr", expr)
	}
	if c.Else == nil {
		t.Fatal("Else is nil in three-arm form")
	}
	target, ok := c.Target.(*compiler.AtomExpr)
	if !ok {
		t.Fatalf("Target type %T", c.Target)
	}
	if target.Name != "repo.merge" {
		t.Errorf("target = %q", target.Name)
	}
	elseAtom, ok := c.Else.(*compiler.AtomExpr)
	if !ok {
		t.Fatalf("Else type %T", c.Else)
	}
	if elseAtom.Name != "human.review" {
		t.Errorf("else = %q", elseAtom.Name)
	}
}

func TestConditional_WithModifiersOnBothArms(t *testing.T) {
	src := `approved ? repo.merge:effect=high_risk : human.review:effect=high_risk`
	expr, err := compiler.NewParser(src).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	c := expr.(*compiler.ConditionalExpr)

	target := c.Target.(*compiler.AtomExpr)
	if target.Name != "repo.merge" {
		t.Errorf("target = %q", target.Name)
	}
	if len(target.Modifiers) != 1 || target.Modifiers[0] != "effect=high_risk" {
		t.Errorf("target modifiers = %v", target.Modifiers)
	}

	elseAtom := c.Else.(*compiler.AtomExpr)
	if elseAtom.Name != "human.review" {
		t.Errorf("else = %q", elseAtom.Name)
	}
	if len(elseAtom.Modifiers) != 1 || elseAtom.Modifiers[0] != "effect=high_risk" {
		t.Errorf("else modifiers = %v", elseAtom.Modifiers)
	}
}

func TestConditional_RightAssociative(t *testing.T) {
	// a ? b : c ? d : e parses as a ? b : (c ? d : e)
	expr, err := compiler.NewParser(`a ? b : c ? d : e`).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	outer := expr.(*compiler.ConditionalExpr)
	if outer.Else == nil {
		t.Fatal("outer.Else is nil")
	}
	inner, ok := outer.Else.(*compiler.ConditionalExpr)
	if !ok {
		t.Fatalf("outer.Else type %T, want ConditionalExpr", outer.Else)
	}
	if inner.Else == nil {
		t.Fatal("inner.Else is nil")
	}
}

// Modifier colon must not be mistaken for a ternary colon.
func TestAtomModifier_GluedColonStillWorks(t *testing.T) {
	expr, err := compiler.NewParser(`repo.merge:effect=high_risk`).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)
	if atom.Name != "repo.merge" {
		t.Errorf("name = %q", atom.Name)
	}
	if len(atom.Modifiers) != 1 || atom.Modifiers[0] != "effect=high_risk" {
		t.Errorf("modifiers = %v", atom.Modifiers)
	}
}

// ─── @{...} structural args ────────────────────────────────────────────

func TestAtBrace_HappyPath(t *testing.T) {
	expr, err := compiler.NewParser(`agent.architect @{ goal: .ticket, feedback: .feedback }`).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)
	if len(atom.Args) != 2 {
		t.Fatalf("args = %v", atom.Args)
	}
	if ref := argRef(t, atom.Args["goal"]); ref != "ticket" {
		t.Errorf("goal ref = %q, want ticket", ref)
	}
	if ref := argRef(t, atom.Args["feedback"]); ref != "feedback" {
		t.Errorf("feedback ref = %q, want feedback", ref)
	}
}

func TestAtBrace_QuotedValuesAreLiteral(t *testing.T) {
	expr, err := compiler.NewParser(`x @{ path: ".literal.not.state" }`).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)
	if s := argStr(t, atom.Args["path"]); s != ".literal.not.state" {
		t.Errorf("path = %q", s)
	}
}

func TestAtBrace_WithModifiers(t *testing.T) {
	src := `agent.architect:provider="deepseek":typed=ArchitectPlan @{ goal: .ticket }`
	expr, err := compiler.NewParser(src).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)
	if len(atom.Modifiers) != 2 {
		t.Errorf("modifiers = %v", atom.Modifiers)
	}
	if ref := argRef(t, atom.Args["goal"]); ref != "ticket" {
		t.Errorf("goal = %q", ref)
	}
}

func TestAtBrace_RejectsDuplicateKey(t *testing.T) {
	_, err := compiler.NewParser(`x @{ a: 1, a: 2 }`).ParseExpression()
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate key error, got %v", err)
	}
}

func TestAtBrace_RejectsMissingColon(t *testing.T) {
	_, err := compiler.NewParser(`x @{ a 1 }`).ParseExpression()
	if err == nil || !strings.Contains(err.Error(), "expected ':'") {
		t.Fatalf("expected missing colon error, got %v", err)
	}
}

func TestAtBrace_EmptyIsNoop(t *testing.T) {
	expr, err := compiler.NewParser(`x @{}`).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)
	if len(atom.Args) != 0 {
		t.Errorf("args = %v, want empty", atom.Args)
	}
}

// ─── @{...} typed values (new) ─────────────────────────────────────────

func TestAtBrace_NestedObject(t *testing.T) {
	src := `decide @{ backend: "laya", questions: { department: { type: "choice", labels: ["a","b","c"] } } }`
	expr, err := compiler.NewParser(src).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)

	q := atom.Args["questions"]
	if q == nil || q.Kind != compiler.ValueMap {
		t.Fatalf("questions kind = %v, want map", q)
	}
	if len(q.Map) != 1 || q.Map[0].Key != "department" {
		t.Fatalf("questions map = %+v", q.Map)
	}

	dept := q.Map[0].Value
	if dept.Kind != compiler.ValueMap {
		t.Fatalf("department kind = %v, want map", dept.Kind)
	}

	var typeStr string
	var labels []*compiler.Value
	for _, e := range dept.Map {
		switch e.Key {
		case "type":
			typeStr = e.Value.Str
		case "labels":
			labels = e.Value.Slice
		}
	}
	if typeStr != "choice" {
		t.Errorf("type = %q, want choice", typeStr)
	}
	if len(labels) != 3 {
		t.Fatalf("labels count = %d, want 3", len(labels))
	}
	for i, want := range []string{"a", "b", "c"} {
		if labels[i].Str != want {
			t.Errorf("labels[%d] = %q, want %q", i, labels[i].Str, want)
		}
	}
}

func TestAtBrace_ArrayOfObjects(t *testing.T) {
	src := `x @{ items: [ { id: "a" }, { id: "b" } ] }`
	expr, err := compiler.NewParser(src).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)

	items := atom.Args["items"]
	if items.Kind != compiler.ValueSlice {
		t.Fatalf("items kind = %v, want slice", items.Kind)
	}
	if len(items.Slice) != 2 {
		t.Fatalf("items count = %d, want 2", len(items.Slice))
	}
	for i, wantID := range []string{"a", "b"} {
		obj := items.Slice[i]
		if obj.Kind != compiler.ValueMap {
			t.Fatalf("item %d kind = %v, want map", i, obj.Kind)
		}
		if len(obj.Map) != 1 || obj.Map[0].Key != "id" || obj.Map[0].Value.Str != wantID {
			t.Errorf("item %d = %+v", i, obj.Map)
		}
	}
}

func TestAtBrace_NumberBoolNull(t *testing.T) {
	src := `x @{ count: 42, ratio: 3.14, enabled: true, missing: null }`
	expr, err := compiler.NewParser(src).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)

	if v := atom.Args["count"]; v.Kind != compiler.ValueNumber || v.Num != 42 {
		t.Errorf("count = %+v", v)
	}
	if v := atom.Args["ratio"]; v.Kind != compiler.ValueNumber || v.Num != 3.14 {
		t.Errorf("ratio = %+v", v)
	}
	if v := atom.Args["enabled"]; v.Kind != compiler.ValueBool || !v.Bool {
		t.Errorf("enabled = %+v", v)
	}
	if v := atom.Args["missing"]; v.Kind != compiler.ValueNull {
		t.Errorf("missing = %+v", v)
	}
}

func TestAtBrace_WholeStateRef(t *testing.T) {
	expr, err := compiler.NewParser(`decide @{ state: . }`).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)
	if !atom.Args["state"].IsWholeStateRef() {
		t.Fatalf("expected whole-state ref, got %+v", atom.Args["state"])
	}
}

func TestAtBrace_CommentsInside(t *testing.T) {
	src := "decide @{\n" +
		"  // which backend to use\n" +
		`  backend: "laya",` + "\n" +
		"  # how many retries\n" +
		"  retries: 3,\n" +
		"  /* nested info */\n" +
		`  questions: { q: "hi" }` + "\n" +
		"}"
	expr, err := compiler.NewParser(src).ParseExpression()
	if err != nil {
		t.Fatalf("comments inside @{} should be allowed: %v", err)
	}
	atom := expr.(*compiler.AtomExpr)
	if len(atom.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(atom.Args))
	}
}

// ─── @prompt vs @{...} ─────────────────────────────────────────────────

func TestAtPrompt_StillWorks(t *testing.T) {
	expr, err := compiler.NewParser(`agent.critic @security review`).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)
	if atom.Prompt != "security review" {
		t.Errorf("prompt = %q", atom.Prompt)
	}
	if len(atom.Args) != 0 {
		t.Errorf("args should be empty for @prompt form, got %v", atom.Args)
	}
}

func TestAtPrompt_AndAtBrace_OnSameAtom(t *testing.T) {
	src := `agent.architect @security @{ goal: .ticket }`
	expr, err := compiler.NewParser(src).ParseExpression()
	if err != nil {
		t.Fatal(err)
	}
	atom := expr.(*compiler.AtomExpr)
	if atom.Prompt != "security" {
		t.Errorf("prompt = %q", atom.Prompt)
	}
	if len(atom.Args) != 1 {
		t.Fatalf("args = %v", atom.Args)
	}
	if ref := argRef(t, atom.Args["goal"]); ref != "ticket" {
		t.Errorf("goal = %q", ref)
	}
}
