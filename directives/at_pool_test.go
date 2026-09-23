package directives

import (
	"strings"
	"testing"
)

func TestAtPool_HappyPath(t *testing.T) {
	ctx := newTestCtx()
	next, err := atPool{}.Apply(ctx, []string{`@pool experts [fast, smart, local]`}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != 1 {
		t.Fatalf("next = %d, want 1", next)
	}
	if len(ctx.Out.Pools) != 1 {
		t.Fatalf("pools = %v", ctx.Out.Pools)
	}
	p := ctx.Out.Pools[0]
	if p.Name != "experts" {
		t.Errorf("name = %q", p.Name)
	}
	if len(p.Members) != 3 || p.Members[1] != "smart" {
		t.Errorf("members = %v", p.Members)
	}
}

func TestAtPool_RejectsEmptyMembers(t *testing.T) {
	ctx := newTestCtx()
	_, err := atPool{}.Apply(ctx, []string{`@pool experts []`}, 0)
	if err == nil || !strings.Contains(err.Error(), "at least one member") {
		t.Fatalf("got %v", err)
	}
}

func TestAtPool_RejectsMissingBracket(t *testing.T) {
	ctx := newTestCtx()
	_, err := atPool{}.Apply(ctx, []string{`@pool experts`}, 0)
	if err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("got %v", err)
	}
}

func TestAtPool_RejectsDuplicate(t *testing.T) {
	ctx := newTestCtx()
	d := atPool{}
	_, _ = d.Apply(ctx, []string{`@pool experts [fast]`}, 0)
	_, err := d.Apply(ctx, []string{`@pool experts [smart]`}, 0)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("got %v", err)
	}
}
