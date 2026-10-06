package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

// mapLoader resolves targets against in-memory maps. resolved maps a
// target to the path reported for cycle detection; when absent, the
// target itself is used.
type mapLoader struct {
	content  map[string]string
	resolved map[string]string
	errs     map[string]error
}

func (l *mapLoader) Load(_, target string) (data []byte, resolvedPath string, err error) {
	if loaderErr, ok := l.errs[target]; ok {
		return nil, "", loaderErr
	}
	content, ok := l.content[target]
	if !ok {
		return nil, "", fmt.Errorf("not found: %s", target)
	}
	resolvedPath = target
	if p, ok := l.resolved[target]; ok {
		resolvedPath = p
	}
	return []byte(content), resolvedPath, nil
}

// testInclude is a minimal directive that delegates to Recurse, so
// tests exercise the loader path without depending on the include
// extension (which would create an import cycle).
func testIncludeDirective() Directive {
	return Directive{
		Name: "test_include",
		Handler: func(_ context.Context, req DirectiveReq) (DirectiveRes, error) {
			line := strings.TrimSpace(req.Lines[req.I])
			target := strings.TrimSpace(strings.TrimPrefix(line, "@test_include"))
			clean, _, err := req.Recurse(target)
			if err != nil {
				return DirectiveRes{}, err
			}
			req.Body[req.I] = clean
			return DirectiveRes{Next: req.I + 1}, nil
		},
	}
}

func TestSourceLoader_HappyPath(t *testing.T) {
	t.Parallel()

	loader := &mapLoader{
		content: map[string]string{"a": "included content"},
	}
	ctx := WithSourceLoader(context.Background(), loader)
	dt := NewDirectiveTable(testIncludeDirective())

	clean, _, err := Preprocess(ctx, dt, "before\n@test_include a\nafter", "root.nflow")
	ktest.RequireNoError(t, err)
	ktest.RequireStringContains(t, clean, "included content")
	ktest.RequireStringContains(t, clean, "before")
	ktest.RequireStringContains(t, clean, "after")
}

func TestSourceLoader_ErrorPropagates(t *testing.T) {
	t.Parallel()

	loader := &mapLoader{
		errs: map[string]error{"a": errors.New("boom")},
	}
	ctx := WithSourceLoader(context.Background(), loader)
	dt := NewDirectiveTable(testIncludeDirective())

	_, _, err := Preprocess(ctx, dt, "@test_include a", "root.nflow")
	ktest.RequireErrorContains(t, err, "boom")
}

func TestSourceLoader_CycleDetected(t *testing.T) {
	t.Parallel()

	loader := &mapLoader{
		content:  map[string]string{"a": "@test_include a\n"},
		resolved: map[string]string{"a": "a"},
	}
	ctx := WithSourceLoader(context.Background(), loader)
	dt := NewDirectiveTable(testIncludeDirective())

	_, _, err := Preprocess(ctx, dt, "@test_include a", "root.nflow")
	ktest.RequireErrorContains(t, err, "cycle detected")
}

func TestSourceLoader_DiamondDependencyAllowed(t *testing.T) {
	t.Parallel()

	//   root
	//   ├── a
	//   └── b
	//       └── a
	// a is included twice but from different branches, not nested.
	loader := &mapLoader{
		content: map[string]string{
			"a": "content-a",
			"b": "@test_include a\ncontent-b",
		},
	}
	ctx := WithSourceLoader(context.Background(), loader)
	dt := NewDirectiveTable(testIncludeDirective())

	source := "@test_include a\n@test_include b"
	clean, _, err := Preprocess(ctx, dt, source, "root.nflow")
	ktest.RequireNoError(t, err)
	ktest.RequireStringContains(t, clean, "content-a")
	ktest.RequireStringContains(t, clean, "content-b")
}
