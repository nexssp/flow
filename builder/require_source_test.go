package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nexssp/flow/extensions/require"
)

const testLatest = "v2.3.4"

func sourceFixture(t *testing.T, source string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "main.nflow")
	if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o640); err != nil {
		t.Fatal(err)
	}
	return file
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func queryLatest(_ context.Context, _ require.Requirement, selector string) (string, error) {
	if selector != "latest" {
		return "", errors.New("unexpected selector: " + selector)
	}
	return testLatest, nil
}

func TestManageRequirementSourceCheckIsReadOnly(t *testing.T) {
	original := "@require example.org/team/alpha v1.0.0\n"
	file := sourceFixture(t, original)
	backup := file + ".bak"
	if err := os.WriteFile(backup, []byte("older undo stage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	goSum := filepath.Join(filepath.Dir(file), "go.sum")
	lock := filepath.Join(filepath.Dir(file), "flow.lock")
	for _, path := range []string{goSum, lock} {
		if err := os.WriteFile(path, []byte("sentinel\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var calls []string
	query := func(_ context.Context, _ require.Requirement, selector string) (string, error) {
		calls = append(calls, selector)
		if selector == "v1.0.0" {
			return "v1.0.0", nil
		}
		return testLatest, nil
	}
	result, err := manageRequirementSource(context.Background(), file, "check", "", nil, query)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Backup != "" || len(result.Changes) != 1 {
		t.Fatalf("check result = %#v; want one outdated declaration and no backup", result)
	}
	if !reflect.DeepEqual(calls, []string{"v1.0.0", "latest"}) {
		t.Fatalf("query selectors = %v, want current then latest", calls)
	}
	for path, want := range map[string]string{file: original, backup: "older undo stage\n", goSum: "sentinel\n", lock: "sentinel\n"} {
		if got := string(readFile(t, path)); got != want {
			t.Errorf("%s changed: got %q, want %q", filepath.Base(path), got, want)
		}
	}
}

func TestManageRequirementSourceUpdateSelectors(t *testing.T) {
	const original = "@require example.org/team/alpha v1.0.0\n@require example.org/team/beta v1.1.0\n@require example.org/other/gamma v1.2.0\n"
	for _, tc := range []struct {
		name, pattern string
		want          []string
	}{
		{name: "all", want: []string{"example.org/team/alpha", "example.org/team/beta", "example.org/other/gamma"}},
		{name: "exact", pattern: "example.org/team/alpha", want: []string{"example.org/team/alpha"}},
		{name: "glob", pattern: "example.org/team/*", want: []string{"example.org/team/alpha", "example.org/team/beta"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := sourceFixture(t, original)
			var queried []string
			query := func(_ context.Context, r require.Requirement, selector string) (string, error) {
				if selector != "latest" {
					return "", errors.New("update should query only latest")
				}
				queried = append(queried, r.Import)
				return testLatest, nil
			}
			result, err := manageRequirementSource(context.Background(), file, "update", tc.pattern, nil, query)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(queried, tc.want) {
				t.Fatalf("queried imports = %v, want %v", queried, tc.want)
			}
			if !result.Changed || result.Backup != file+".bak" {
				t.Fatalf("update result = %#v, want changed with backup", result)
			}
			got := string(readFile(t, file))
			for _, name := range tc.want {
				line := "@require " + name + " " + testLatest + "\n"
				if !strings.Contains(got, line) {
					t.Errorf("updated source missing %q:\n%s", line, got)
				}
			}
			if tc.pattern != "" && tc.name != "all" {
				for _, name := range []string{"example.org/team/alpha", "example.org/team/beta", "example.org/other/gamma"} {
					if !slices.Contains(tc.want, name) && !strings.Contains(got, "@require "+name+" ") {
						t.Errorf("unselected declaration %s was lost: %s", name, got)
					}
				}
			}
		})
	}
}

func TestManageRequirementSourcePinQueriesOriginalSelectorsOnly(t *testing.T) {
	original := "@require example.org/tagged v1.2.0\n@require example.org/branch release/stable\n@require example.org/commit 0123456789abcdef0123456789abcdef01234567\n"
	file := sourceFixture(t, original)
	var seen []string
	query := func(_ context.Context, r require.Requirement, selector string) (string, error) {
		seen = append(seen, r.Import+"@"+selector)
		return "v1.2.3", nil
	}
	result, err := manageRequirementSource(context.Background(), file, "pin", "", nil, query)
	if err != nil {
		t.Fatal(err)
	}
	wantQueries := []string{"example.org/tagged@v1.2.0", "example.org/branch@release/stable", "example.org/commit@0123456789abcdef0123456789abcdef01234567"}
	if !reflect.DeepEqual(seen, wantQueries) {
		t.Fatalf("query calls = %v, want original selectors only: %v", seen, wantQueries)
	}
	if got := string(readFile(t, file)); got != "@require example.org/tagged v1.2.3\n@require example.org/branch v1.2.3\n@require example.org/commit v1.2.3\n" {
		t.Fatalf("pinned source = %q", got)
	}
	if !result.Changed || result.Backup != file+".bak" {
		t.Fatalf("pin result = %#v", result)
	}
}

func TestManageRequirementSourceSkipsBuiltinsLocalAndSameModule(t *testing.T) {
	dir := t.TempDir()
	localDir := filepath.Join(dir, "local")
	if err := os.Mkdir(localDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "go.mod"), []byte("module example.org/local\n\ngo 1.20\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.nflow")
	if err := os.WriteFile(file, []byte("@require selftestkit v1.0.0\n@require ./local\n@require github.com/nexssp/flow/extensions/macros v1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	query := func(context.Context, require.Requirement, string) (string, error) {
		called = true
		return testLatest, nil
	}
	result, err := manageRequirementSource(context.Background(), file, "update", "*", nil, query)
	if err != nil {
		t.Fatal(err)
	}
	if called || result.Changed || len(result.Changes) != 3 {
		t.Fatalf("skip result = %#v, queried=%v; expected all declarations skipped", result, called)
	}
	for _, change := range result.Changes {
		if change.Reason == "" {
			t.Errorf("%s was not classified as skipped", change.Import)
		}
	}
	if got := string(readFile(t, file)); !strings.Contains(got, "v1.0.0") || strings.Contains(got, testLatest) {
		t.Fatalf("skipped source modified: %s", got)
	}
}

func TestManageRequirementSourcePreservesFormattingCommentsAliasesOptionsAndCRLF(t *testing.T) {
	original := "# header\r\n@require \"example.org/team/quoted\" 'main' as quotes {\r\n  timeout: \"5s\"\r\n}\r\n@require example.org/team/insert as insert # retain this comment\r\n"
	file := sourceFixture(t, original)
	query := func(_ context.Context, _ require.Requirement, selector string) (string, error) {
		if selector != "latest" {
			return "", errors.New("update should use latest")
		}
		return testLatest, nil
	}
	_, err := manageRequirementSource(context.Background(), file, "update", "", nil, query)
	if err != nil {
		t.Fatal(err)
	}
	got := string(readFile(t, file))
	want := "# header\r\n@require \"example.org/team/quoted\" '" + testLatest + "' as quotes {\r\n  timeout: \"5s\"\r\n}\r\n@require example.org/team/insert " + testLatest + " as insert # retain this comment\r\n"
	if got != want {
		t.Fatalf("formatted source mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestManageRequirementSourceUpdatesDuplicateImportsAndIgnoresMacroAndIncludedChild(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.nflow")
	childText := "@require example.org/child v1.0.0\n"
	if err := os.WriteFile(child, []byte(childText), 0o600); err != nil {
		t.Fatal(err)
	}
	original := "@macro dormant() {\n  @require example.org/macro v1.0.0\n}\n@include ./child.nflow\n@require example.org/duplicate v1.0.0\n@require example.org/duplicate v1.1.0\n"
	file := filepath.Join(dir, "main.nflow")
	if err := os.WriteFile(file, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	query := func(_ context.Context, _ require.Requirement, selector string) (string, error) {
		if selector != "latest" {
			return "", errors.New("unexpected selector")
		}
		return testLatest, nil
	}
	_, err := manageRequirementSource(context.Background(), file, "update", "", nil, query)
	if err != nil {
		t.Fatal(err)
	}
	got := string(readFile(t, file))
	if !strings.Contains(got, "@require example.org/duplicate "+testLatest) || strings.Count(got, "@require example.org/duplicate "+testLatest) != 2 {
		t.Fatalf("both direct duplicate declarations should update:\n%s", got)
	}
	if !strings.Contains(got, "@require example.org/macro v1.0.0") {
		t.Fatalf("captured macro body declaration should remain untouched:\n%s", got)
	}
	if childGot := string(readFile(t, child)); childGot != childText {
		t.Fatalf("included child changed: got %q, want %q", childGot, childText)
	}
}

func TestManageRequirementSourceErrorsAreAllOrNothingAndRejectPatterns(t *testing.T) {
	original := "@require example.org/first v1.0.0\n@require example.org/second v1.0.0\n"
	file := sourceFixture(t, original)
	backup := file + ".bak"
	if err := os.WriteFile(backup, []byte("prior backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	query := func(_ context.Context, _ require.Requirement, _ string) (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("resolver failed")
		}
		return testLatest, nil
	}
	if _, err := manageRequirementSource(context.Background(), file, "update", "", nil, query); err == nil || !strings.Contains(err.Error(), "resolver failed") {
		t.Fatalf("second resolution error = %v", err)
	}
	if got := string(readFile(t, file)); got != original {
		t.Fatalf("partial update published after query failure: %q", got)
	}
	if got := string(readFile(t, backup)); got != "prior backup\n" {
		t.Fatalf("backup changed after query failure: %q", got)
	}
	for _, pattern := range []string{"[", "example.org/absent/*"} {
		if _, err := manageRequirementSource(context.Background(), file, "update", pattern, nil, queryLatest); err == nil {
			t.Errorf("pattern %q unexpectedly succeeded", pattern)
		}
		if got := string(readFile(t, file)); got != original {
			t.Errorf("pattern %q changed source: %q", pattern, got)
		}
	}
}

func TestManageRequirementSourceNoOpKeepsBackupAndSecondStageCanReset(t *testing.T) {
	original := "@require example.org/already v2.3.4\n"
	file := sourceFixture(t, original)
	backup := file + ".bak"
	if err := os.WriteFile(backup, []byte("preserve this backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	query := func(_ context.Context, _ require.Requirement, selector string) (string, error) {
		if selector == "v2.3.4" {
			return testLatest, nil
		}
		return testLatest, nil
	}
	result, err := manageRequirementSource(context.Background(), file, "update", "", nil, query)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || string(readFile(t, backup)) != "preserve this backup\n" {
		t.Fatalf("no-op did not preserve old stage: result=%#v backup=%q", result, readFile(t, backup))
	}
	stageLatest := func(context.Context, require.Requirement, string) (string, error) { return "v3.4.5", nil }
	if _, err := manageRequirementSource(context.Background(), file, "update", "", nil, stageLatest); err != nil {
		t.Fatal(err)
	}
	staged := "@require example.org/already v3.4.5\n"
	if got := string(readFile(t, file)); got != staged {
		t.Fatalf("staged source = %q, want %q", got, staged)
	}
	if got := string(readFile(t, backup)); got != original {
		t.Fatalf("new stage did not overwrite backup with prior source: %q", got)
	}
	if err := resetRequirementSource(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if got := string(readFile(t, file)); got != original {
		t.Fatalf("reset source = %q, want %q", got, original)
	}
	if _, err := os.Lstat(backup); !os.IsNotExist(err) {
		t.Fatalf("reset should consume backup; lstat error = %v", err)
	}
}

func TestManageRequirementSourceCancellationDoesNotWrite(t *testing.T) {
	original := "@require example.org/cancel v1.0.0\n"
	file := sourceFixture(t, original)
	ctx, cancel := context.WithCancel(context.Background())
	query := func(context.Context, require.Requirement, string) (string, error) {
		cancel()
		return testLatest, nil
	}
	if _, err := manageRequirementSource(ctx, file, "update", "", nil, query); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled operation error = %v", err)
	}
	if got := string(readFile(t, file)); got != original {
		t.Fatalf("canceled operation changed source: %q", got)
	}
	if _, err := os.Lstat(file + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("canceled operation created backup: %v", err)
	}
}

func TestManageRequirementSourceRejectsSymlinkSourceAndBackup(t *testing.T) {
	t.Run("source", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.nflow")
		if err := os.WriteFile(target, []byte("@require example.org/link v1.0.0\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "main.nflow")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := manageRequirementSource(context.Background(), link, "update", "", nil, queryLatest); err == nil {
			t.Fatal("symlink source unexpectedly accepted")
		}
		if got := string(readFile(t, target)); !strings.Contains(got, "v1.0.0") {
			t.Fatalf("target modified through symlink: %q", got)
		}
	})
	t.Run("backup", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "main.nflow")
		original := "@require example.org/link v1.0.0\n"
		if err := os.WriteFile(file, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "backup-target")
		if err := os.WriteFile(target, []byte("do not overwrite\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, file+".bak"); err != nil {
			t.Fatal(err)
		}
		if _, err := manageRequirementSource(context.Background(), file, "update", "", nil, queryLatest); err == nil {
			t.Fatal("symlink backup unexpectedly accepted")
		}
		if got := string(readFile(t, file)); got != original {
			t.Fatalf("source changed with symlink backup: %q", got)
		}
		if got := string(readFile(t, target)); got != "do not overwrite\n" {
			t.Fatalf("backup target changed: %q", got)
		}
	})
}

func TestManageRequirementSourceDetectsConcurrentSourceEdit(t *testing.T) {
	original := "@require example.org/concurrent v1.0.0\n"
	file := sourceFixture(t, original)
	concurrent := "# concurrent editor won\n" + original
	query := func(_ context.Context, _ require.Requirement, _ string) (string, error) {
		if err := os.WriteFile(file, []byte(concurrent), 0o600); err != nil {
			return "", err
		}
		return testLatest, nil
	}
	if _, err := manageRequirementSource(context.Background(), file, "update", "", nil, query); err == nil || !strings.Contains(err.Error(), "source changed during resolution") {
		t.Fatalf("concurrent-edit error = %v", err)
	}
	if got := string(readFile(t, file)); got != concurrent {
		t.Fatalf("concurrent content was not preserved: %q", got)
	}
	if _, err := os.Lstat(file + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("concurrent edit created backup: %v", err)
	}
}

func TestManageRequirementSourceRejectsComputedHeaderEdits(t *testing.T) {
	original := "@const VERSION = \"main\"\n@require example.org/team/alpha ${VERSION}\n"
	file := sourceFixture(t, original)
	query := func(_ context.Context, _ require.Requirement, selector string) (string, error) {
		if selector != "main" {
			t.Fatalf("selector = %q, want substituted main", selector)
		}
		return "v1.0.0", nil
	}
	_, err := manageRequirementSource(t.Context(), file, "pin", "", nil, query)
	if err == nil || !strings.Contains(err.Error(), "header changed by preprocessing") {
		t.Fatalf("error = %v", err)
	}
	if got := string(readFile(t, file)); got != original {
		t.Fatalf("computed header was modified: %q", got)
	}
	if _, err := os.Stat(file + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup: %v", err)
	}
}

func TestRequirementBackupIsNotHarnessInput(t *testing.T) {
	file := sourceFixture(t, "@require runtime\n")
	dir := filepath.Dir(file)
	before := moduleSourceFingerprint(dir)
	if err := os.WriteFile(file+".bak", []byte("older source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := moduleSourceFingerprint(dir); got != before {
		t.Fatal("optional backup changed harness build inputs")
	}
	if err := os.WriteFile(file+".bak", []byte("another previous stage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := moduleSourceFingerprint(dir); got != before {
		t.Fatal("backup content changed harness build inputs")
	}
	if err := os.Remove(file + ".bak"); err != nil {
		t.Fatal(err)
	}
	if got := moduleSourceFingerprint(dir); got != before {
		t.Fatal("removing optional backup changed harness build inputs")
	}
}

func TestManageRequirementSourceAcceptsCanonicalIncompatibleVersion(t *testing.T) {
	file := sourceFixture(t, "@require example.org/team/alpha main\n")
	query := func(context.Context, require.Requirement, string) (string, error) { return "v2.0.0+incompatible", nil }
	result, err := manageRequirementSource(t.Context(), file, "pin", "", nil, query)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !strings.Contains(string(readFile(t, file)), "v2.0.0+incompatible") {
		t.Fatalf("result = %#v", result)
	}
}

func TestManageRequirementSourcePreservesSourceAndWorkspaceProviders(t *testing.T) {
	for _, provider := range []string{"source-module", "workspace-use", "workspace-replace"} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			moduleDir := dir
			modulePath := "example.org/precedence"
			if provider != "source-module" {
				moduleDir = filepath.Join(dir, "provider")
				if err := os.Mkdir(moduleDir, 0o700); err != nil {
					t.Fatal(err)
				}
				work := "go 1.26.9\nuse ./provider\n"
				if provider == "workspace-replace" {
					work = "go 1.26.9\nreplace example.org/precedence => ./provider\n"
				}
				if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte(work), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module "+modulePath+"\ngo 1.26.9\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "main.nflow")
			original := "@require example.org/precedence main\n"
			if err := os.WriteFile(file, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			query := func(context.Context, require.Requirement, string) (string, error) {
				t.Fatal("local provider must not cause a remote query")
				return "", nil
			}
			for _, operation := range []string{"check", "update", "pin"} {
				result, err := manageRequirementSource(t.Context(), file, operation, "", nil, query)
				if err != nil {
					t.Fatal(err)
				}
				if result.Changed || len(result.Changes) != 1 || result.Changes[0].Reason != "local/workspace provider" {
					t.Fatalf("result = %#v", result)
				}
			}
			if string(readFile(t, file)) != original {
				t.Fatal("local provider source was changed")
			}
		})
	}
}
