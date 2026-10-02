package core

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestBuildCatalogIncludesMountedCanonicalActions(t *testing.T) {
	t.Parallel()

	probe := action.New("catalog.probe", func(_ context.Context, in any) (any, error) {
		return in, nil
	}).Description("catalog probe").Build()

	resolver, err := NewDynamicResolver(action.Library{
		Name:    "catalog_test",
		Actions: []action.AnyAction{probe},
	})
	if err != nil {
		t.Fatalf("NewDynamicResolver() error = %v", err)
	}

	catalog := BuildCatalog(resolver, NewModifierTable(), NewDirectiveTable(), NewOperatorTable())
	got := make(map[string]bool, len(catalog.Atoms))
	for _, atom := range catalog.Atoms {
		got[atom.Name] = true
	}
	for _, name := range []string{"catalog.probe"} {
		if !got[name] {
			t.Errorf("catalog is missing mounted action name %q", name)
		}
	}
}
