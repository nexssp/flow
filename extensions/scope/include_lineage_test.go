package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	includeext "github.com/nexssp/flow/extensions/include"
)

func TestIncludeRetainsScopedPoliciesAndFollowingLineNumbers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	main := filepath.Join(root, "main.nflow")
	child := "@scope :timeout=1s {\n@include leaf.nflow\n}\n"
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(root, "child.nflow"), []byte(child), 0o600))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(root, "leaf.nflow"), []byte("leaf.action\n"), 0o600))
	source := "@scope :timeout=5s {\n@include child.nflow\n}\n@scope :retry=2 {\nroot.action\n}\n"
	table := compiler.NewDirectiveTable(Directive, includeext.Directive)
	clean, metadata, err := compiler.Preprocess(t.Context(), table, source, main)
	ktest.RequireNoError(t, err)
	lookups := makeLookups(spansFromMeta(metadata), compiler.SourceLinesFromMeta(metadata))
	found := make(map[string][][]string)
	for i, line := range strings.Split(clean, "\n") {
		name := strings.TrimSpace(line)
		if name != "leaf.action" && name != "root.action" {
			continue
		}
		for _, lookup := range lookups {
			if modifiers := lookup.Fn(i + 1); len(modifiers) > 0 {
				found[name] = append(found[name], modifiers)
			}
		}
	}
	ktest.RequireEqual(t, found["leaf.action"], [][]string{{"timeout=5s"}, {"timeout=1s"}})
	ktest.RequireEqual(t, found["root.action"], [][]string{{"retry=2"}})
}
