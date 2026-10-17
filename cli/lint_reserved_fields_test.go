package cli

import (
	"strings"
	"testing"
)

func TestLintReservedFieldsAndBuiltinCalls(t *testing.T) {
	for _, source := range []string{
		`{ count: 5, type: "invoice", len: 2, min: 3 } -> { ok: .count > 4 && .type == "invoice" && .len > 0 && .min < 10, size: len([1, 2]) }`,
		`{ count: 5 } -> assert(.count > 4, "count")`,
		`{ count: 0 } -> loop({ count: .count + 1 }) until(.count > 4)`,
	} {
		if issues := lintOne(t, source); len(issues) != 0 {
			t.Errorf("lint %q = %#v, want valid expression", source, issues)
		}
	}
}

func TestLintBareBuiltinHasFieldHint(t *testing.T) {
	for _, source := range []string{
		`{ ok: count > 4 }`,
		`{ child: race { "ok" }, ok: count > 4 }`,
		`assert(count > 4, "missing dot")`,
		`loop({ count: 5 }) until(count > 4)`,
		`match { count > 4 -> "yes", _ -> "no" }`,
	} {
		issues := lintOne(t, source)
		if len(issues) != 1 || issues[0].Kind != "compile" || !strings.Contains(issues[0].Message, "for a payload field use .count") {
			t.Errorf("lint %q = %#v, want compile-time field hint", source, issues)
		}
	}
}
