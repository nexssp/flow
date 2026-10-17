package compiler

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestParseDeclarationHeader(t *testing.T) {
	t.Parallel()
	for _, directive := range []string{"profile", "pipeline"} {
		t.Run(directive, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				source string
				mods   []string
				next   int
			}{
				{"inline", " work:retry=2:idempotent :timeout=4s", []string{"retry=2", "idempotent", "timeout=4s"}, 1},
				{"trivia", " work # header\n\n  # policy\n  :retry=2 // retries\n\n  // flags\n  :idempotent :dedup\n\n  runtime.const\n    :timeout=1s", []string{"retry=2", "idempotent", "dedup"}, 7},
				{"quoted", " work\n  :route=\"POST /jobs/:id\" :desc='Maks: ready # // Julia'\n  :tag=`core,slow`", []string{"route=POST /jobs/:id", "desc=Maks: ready # // Julia", "tag=core,slow"}, 3},
				{"body boundary", " work\n  runtime.const\n\n  # atom policy\n  :timeout=2s", nil, 1},
				{"directive boundary", " work\n@profile other\n  :timeout=2s", nil, 1},
				{"macro boundary", " work\n  @guard()\n  :timeout=2s", nil, 1},
				{"unindented boundary", " work\n:timeout=2s", nil, 1},
				{"no continuation", " work\n\n# main\n{ value: 1 }", nil, 1},
				{"eof trivia", " work\n  :retry=2\n\n# done", []string{"retry=2"}, 2},
				{"empty quoted", " work :desc=\"\"", []string{"desc="}, 1},
				{"escaped quote", " work :desc=\"say \\\"yes\\\": ready\" :dedup", []string{"desc=say \"yes\": ready", "dedup"}, 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					source := "@" + directive + tc.source
					lines := strings.Split(source, "\n")
					name, mods, next, err := ParseDeclarationHeader(DirectiveReq{Lines: lines, File: "header.nflow"}, directive)
					ktest.RequireNoError(t, err)
					ktest.RequireEqual(t, name, "work")
					ktest.RequireEqual(t, mods, tc.mods)
					ktest.RequireEqual(t, next, tc.next)
					ktest.RequireEqual(t, strings.Join(lines, "\n"), source)
				})
			}
		})
	}
}

func TestParseDeclarationHeaderRejectsMalformedModifiers(t *testing.T) {
	t.Parallel()
	for _, tail := range []string{":", ":=2", ":retry=", ":retry= :dedup", ":retry= # missing", ":desc=\"unclosed", ":retry=2 garbage", ":dedup -> body"} {
		t.Run(tail, func(t *testing.T) {
			_, _, _, err := ParseDeclarationHeader(DirectiveReq{
				Lines: []string{"@profile p", "", "  " + tail}, File: "bad.nflow",
			}, "profile")
			ktest.RequireErrorContains(t, err, "bad.nflow:3")
		})
	}
}

func TestParseDeclarationHeaderNestedIndent(t *testing.T) {
	t.Parallel()
	_, mods, next, err := ParseDeclarationHeader(DirectiveReq{Lines: []string{
		"  @pipeline work", "    :retry=2", "  :dedup", "    :timeout=1s",
	}}, "pipeline")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, mods, []string{"retry=2"})
	ktest.RequireEqual(t, next, 2)
}
