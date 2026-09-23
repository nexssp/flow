package nodes

import "testing"

func TestPreprocessDotNotation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		in       string
		want     string
		wantRoot bool
	}{
		{"simple leading dot", `.game_over == false`, `game_over == false`, false},
		{"nested leading dot", `.a.b.c > 0`, `a.b.c > 0`, false},
		{"two references", `.take >= 1 && .take <= 3`, `take >= 1 && take <= 3`, false},
		{"index access", `.board[0] == "X"`, `board[0] == "X"`, false},
		{"decimal preserved", `.price > 1.5`, `price > 1.5`, false},
		{"string literal untouched", `.v == "v1.2.3"`, `v == "v1.2.3"`, false},
		{"single-quoted untouched", `.v == 'a.b.c'`, `v == 'a.b.c'`, false},
		{"backtick untouched", ".v == `raw.string.here`", "v == `raw.string.here`", false},
		{"escaped quote inside string", `.v == "a\"b.c"`, `v == "a\"b.c"`, false},
		{"bare dot becomes root", `len(.) > 0`, `len(__root__) > 0`, true},
		{"member access preserved", `foo.bar`, `foo.bar`, false},
		{"call result member", `f().x`, `f().x`, false},
		{"index then member", `a[0].b`, `a[0].b`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, root := PreprocessDotNotation(tc.in)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if root != tc.wantRoot {
				t.Errorf("usesRoot = %v, want %v", root, tc.wantRoot)
			}
		})
	}
}
