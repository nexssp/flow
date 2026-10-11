package native_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/native"
)

func TestSelftest_EveryBundleFixtureUsesOnlyRegisteredDirectives(t *testing.T) {
	bundles := native.SelftestBundles()

	// Build the union of all directives registered by the native set.
	known := map[string]bool{}
	for _, b := range bundles {
		for _, d := range b.Directives {
			known[d.Name] = true
		}
	}

	// For every embedded fixture, scan lines and assert that every
	// `@name` at column 0 names a directive that is registered in the
	// self-test environment, or a macro invocation. Catches the class of bug where
	// a bundle ships a fixture that requires a directive nobody
	// registered — the same failure mode that put @hook:ai.error_analyzer
	// in a fixture before the hook bundle was in native.Bundles().
	//
	// Macro invocations (`@labeled(args)`) are skipped: they are
	// grammar-level, not directive-level. The terminator tells them
	// apart — a directive ends its name with whitespace, `:`, `=`, or
	// `{`; a macro call ends it with `(`.
	for _, b := range bundles {
		if b.Fixtures == nil {
			continue
		}
		_ = fs.WalkDir(b.Fixtures, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".nflow") {
				return err
			}
			data, readErr := fs.ReadFile(b.Fixtures, p)
			ktest.RequireNoError(t, readErr)

			lineNo := 0
			for line := range strings.SplitSeq(string(data), "\n") {
				lineNo++
				trimmed := strings.TrimSpace(line)
				if !strings.HasPrefix(trimmed, "@") || strings.HasPrefix(trimmed, "@{") {
					continue
				}
				name, term := directiveNameFromLine(trimmed)
				if name == "" || term == '(' {
					continue
				}
				if known[name] {
					continue
				}
				t.Errorf("bundle %q fixture %s:%d uses unknown directive @%s",
					b.ID, p, lineNo, name)
			}
			return nil
		})
	}
}

func TestSelftest_EveryExtensionHasNFlowFixtures(t *testing.T) {
	for _, b := range native.SelftestBundles() {
		t.Run(b.ID, func(t *testing.T) {
			if b.Fixtures == nil {
				t.Fatalf("bundle %q has no embedded Fixtures filesystem", b.ID)
			}

			var count int
			err := fs.WalkDir(b.Fixtures, ".", func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if !entry.IsDir() && strings.HasSuffix(path, ".nflow") {
					count++
				}
				return nil
			})
			ktest.RequireNoError(t, err)
			ktest.RequireCondition(t, count > 0, "bundle %q has no embedded .nflow fixture", b.ID)
		})
	}
}

func directiveNameFromLine(line string) (name string, terminator byte) {
	if len(line) < 2 || line[0] != '@' {
		return "", 0
	}
	rest := line[1:]
	for i := range len(rest) {
		switch rest[i] {
		case ' ', '\t', ':', '=':
			return rest[:i], rest[i]
		case '(':
			// Macro invocation — not a directive.
			return rest[:i], '('
		case '{':
			// `@{...}` — excluded by the caller, but return cleanly.
			return rest[:i], '{'
		}
	}
	return rest, 0
}
