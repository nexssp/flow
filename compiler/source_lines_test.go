package compiler

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestEmbeddedSourceRetainsLineAncestry(t *testing.T) {
	t.Parallel()
	loader := &mapLoader{content: map[string]string{"child": "first\n\nlast"}}
	directive := Directive{Name: "embed", Handler: func(_ context.Context, req DirectiveReq) (DirectiveRes, error) {
		clean, metadata, err := req.Recurse("child")
		if err != nil {
			return DirectiveRes{}, err
		}
		req.EmbedSource(clean, metadata)
		return DirectiveRes{Next: req.I + 1}, nil
	}}
	ctx := WithSourceLoader(t.Context(), loader)
	clean, metadata, err := Preprocess(ctx, NewDirectiveTable(directive), "@embed\nroot.action", "root")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, clean, "first\n\nlast\nroot.action")
	lineage := SourceLinesFromMeta(metadata)
	ktest.RequireEqual(t, len(lineage), 4)
	ktest.RequireEqual(t, lineage[0].Origins, []Position{{File: "root", Line: 1}, {File: "child", Line: 1}})
	ktest.RequireEqual(t, lineage[2].Origins, []Position{{File: "root", Line: 1}, {File: "child", Line: 3}})
	ktest.RequireEqual(t, lineage[3].Origins, []Position{{File: "root", Line: 2}})
}

func TestEmbedSourceStandaloneDirectiveRequest(t *testing.T) {
	t.Parallel()
	body := []string{""}
	req := DirectiveReq{Body: body}
	req.EmbedSource("child\nbody", nil)
	ktest.RequireEqual(t, body[0], "child\nbody")
}
