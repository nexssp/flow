package schema

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresDirectiveModifierFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Directives), 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "schema")
	ktest.RequireEqual(t, len(b.Modifiers), 5)
	ktest.RequireEqual(t, b.Modifiers[0].Name, SchemaModifierName)
	ktest.RequireEqual(t, b.Modifiers[1].Name, RequestSchemaModifierName)
	ktest.RequireEqual(t, b.Modifiers[2].Name, ResponseSchemaModifierName)
	ktest.RequireEqual(t, b.Modifiers[3].Name, InSchemaModifierName)
	ktest.RequireEqual(t, b.Modifiers[4].Name, OutSchemaModifierName)
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
}

func TestFixtures_Discoverable(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	var count int
	walkErr := fs.WalkDir(b.Fixtures, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".nflow") {
			return nil
		}
		content, readErr := fs.ReadFile(b.Fixtures, p)
		ktest.RequireNoError(t, readErr)
		ktest.RequireCondition(t, strings.Contains(string(content), "@description"),
			"fixture %q has no @description", p)
		count++
		return nil
	})
	ktest.RequireNoError(t, walkErr)
	ktest.RequireCondition(t, count > 0, "no fixtures embedded")
}
