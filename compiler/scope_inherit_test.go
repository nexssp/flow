package compiler

import (
	"context"
	"errors"
	"testing"
)

func TestCompileInheritsExtensionScopeBeforeDirectives(t *testing.T) {
	t.Parallel()
	parentMeta := map[string]any{"extension.example": "parent declaration"}
	var directiveSawScope bool
	directives := NewDirectiveTable(Directive{
		Name: "requirescope",
		Handler: func(_ context.Context, req DirectiveReq) (DirectiveRes, error) {
			if req.Out["extension.example"] != "parent declaration" {
				return DirectiveRes{}, errors.New("inherited declaration missing before child directive")
			}
			directiveSawScope = true
			return DirectiveRes{Next: req.I + 1, BlankLines: []int{req.I}}, nil
		},
	})
	inherit := ScopeInheritFunc(func(parent, child map[string]any) error {
		if declaration, ok := parent["extension.example"]; ok {
			child["extension.example"] = declaration
		}
		return nil
	})
	compile := CompileAction(nil, directives, nil, nil, nil, WithScopeInheritors(inherit))
	result, err := compile.Do(context.Background(), CompileReq{
		Source:        "@requirescope\n",
		Name:          "child",
		InheritedMeta: parentMeta,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !directiveSawScope {
		t.Fatal("child directive did not observe inherited metadata")
	}
	if result.Meta["extension.example"] != "parent declaration" {
		t.Fatalf("compiled child metadata = %#v", result.Meta)
	}
	if len(parentMeta) != 1 {
		t.Fatalf("parent metadata was mutated: %#v", parentMeta)
	}
}
