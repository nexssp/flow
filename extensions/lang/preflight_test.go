package lang

import (
	"errors"
	"strings"
	"testing"
)

// TestPreflightAllPresent asserts preflight returns an OK report when every
// block's interpreter is on PATH. Skips if python3 is not installed.
func TestPreflightAllPresent(t *testing.T) {
	// Ensure runners loaded (other tests may have cleared the registry).
	if len(Registry) == 0 {
		if err := LoadRunners(); err != nil {
			t.Fatalf("LoadRunners: %v", err)
		}
	}
	if _, _, err := selectPython3ForTest(); err != nil {
		t.Skipf("python3 not installed: %v", err)
	}

	blocks := []*LangBlock{
		{ID: "python", Version: ""},
		{ID: "python", Version: "3"},
	}
	r := Preflight{}.Run(blocks)
	if !r.OK() {
		t.Errorf("OK=false; want true; missing=%v", r.Missing)
	}
	if len(r.Checked) != 2 {
		t.Errorf("Checked = %d; want 2", len(r.Checked))
	}
}

// TestPreflightMissingGroups asserts preflight collects every missing
// interpreter into a single Report.Missing list, not just the first one.
func TestPreflightMissingGroups(t *testing.T) {
	// Register a fake language that always misses, so the test doesn't
	// depend on what's installed on this machine.
	const fakeID = "preflight-fake-lang"
	if _, dup := Registry[fakeID]; dup {
		t.Fatalf("test setup: %q already registered", fakeID)
	}
	Register(Language{
		ID:     fakeID,
		Ext:    ".fake",
		Select: func(string) (string, []string, error) { return "", nil, ErrNotFound },
	})
	defer delete(Registry, fakeID)

	blocks := []*LangBlock{
		{ID: fakeID, Version: ""},
		{ID: fakeID, Version: ""}, // duplicate — should dedupe
		{ID: fakeID, Version: "v2"},
	}
	r := Preflight{}.Run(blocks)
	if r.OK() {
		t.Fatal("OK=true; want false (fake language always misses)")
	}
	if len(r.Missing) != 2 {
		t.Errorf("Missing = %d; want 2 (deduped by id@version)", len(r.Missing))
	}
}

// TestPreflightUnknownLanguage asserts preflight surfaces unknown languages
// (not just missing interpreters) in the Missing list.
func TestPreflightUnknownLanguage(t *testing.T) {
	blocks := []*LangBlock{{ID: "totally-unknown-lang", Version: ""}}
	r := Preflight{}.Run(blocks)
	if r.OK() {
		t.Fatal("OK=true; want false")
	}
	if len(r.Missing) != 1 {
		t.Fatalf("Missing = %d; want 1", len(r.Missing))
	}
	if r.Missing[0].ID != "totally-unknown-lang" {
		t.Errorf("Missing[0].ID = %q", r.Missing[0].ID)
	}
}

// TestPreflightAutoInstall asserts that when an Installer hook is set,
// preflight calls it for each missing entry and updates the Check on success.
func TestPreflightAutoInstall(t *testing.T) {
	const fakeID = "preflight-autoinstall-lang"
	if _, dup := Registry[fakeID]; dup {
		t.Fatalf("test setup: %q already registered", fakeID)
	}
	installCalls := 0
	Register(Language{
		ID:     fakeID,
		Ext:    ".fake",
		Select: func(string) (string, []string, error) { return "", nil, ErrNotFound },
	})
	defer delete(Registry, fakeID)

	pf := Preflight{
		Installer: installHookFunc(func(lang *Language, _ string) (string, []string, error) {
			installCalls++
			return "/usr/bin/installed-" + lang.ID, nil, nil
		}),
	}
	blocks := []*LangBlock{{ID: fakeID, Version: ""}}
	r := pf.Run(blocks)
	if !r.OK() {
		t.Errorf("OK=false; want true (auto-install should have resolved)")
	}
	if installCalls != 1 {
		t.Errorf("install called %d times; want 1", installCalls)
	}
	if r.Checked[0].Cmd != "/usr/bin/installed-"+fakeID {
		t.Errorf("Cmd = %q; want installed path", r.Checked[0].Cmd)
	}
}

// TestPreflightReportError asserts the Report renders a useful multi-line
// message when multiple interpreters are missing.
func TestPreflightReportError(t *testing.T) {
	r := Report{
		Missing: []Check{
			{ID: "python", Version: "3", Tag: "py3"},
			{ID: "rust", Version: "", Tag: "rs"},
		},
	}
	got := r.Error()
	if !strings.Contains(got, "lang.python@3") {
		t.Errorf("Error() missing python@3: %q", got)
	}
	if !strings.Contains(got, "lang.rust") {
		t.Errorf("Error() missing rust: %q", got)
	}
	if !strings.Contains(got, "keyword py3") {
		t.Errorf("Error() missing py3 keyword hint: %q", got)
	}
}

// TestPreflightReportOK asserts the Report renders empty string when OK.
func TestPreflightReportOK(t *testing.T) {
	r := Report{}
	if got := r.Error(); got != "" {
		t.Errorf("Error() on OK report = %q; want empty", got)
	}
}

// TestPreflightSorted asserts Checked and Missing are sorted by (ID, Version).
func TestPreflightSorted(t *testing.T) {
	blocks := []*LangBlock{
		{ID: "go", Version: ""},
		{ID: "python", Version: "3"},
		{ID: "python", Version: ""},
		{ID: "rust", Version: ""},
	}
	r := Preflight{}.Run(blocks)
	for i := 1; i < len(r.Checked); i++ {
		prev, cur := r.Checked[i-1], r.Checked[i]
		if prev.ID > cur.ID || (prev.ID == cur.ID && prev.Version > cur.Version) {
			t.Errorf("Checked not sorted: %v before %v", prev, cur)
		}
	}
}

// --- helpers -------------------------------------------------------------

// installHookFunc adapts a function into the Ensure interface for tests.
type installHookFunc func(*Language, string) (string, []string, error)

func (f installHookFunc) Ensure(lang *Language, version string) (cmd string, args []string, err error) {
	return f(lang, version)
}

// selectPython3ForTest loads runners and tries python3 on PATH. Returns
// ErrNotFound if missing.
func selectPython3ForTest() (cmd string, args []string, err error) {
	if len(Registry) == 0 {
		if loadErr := LoadRunners(); loadErr != nil {
			return "", nil, loadErr
		}
	}
	if language, lookupErr := Lookup("python", "3"); lookupErr == nil {
		return language.Select("3")
	}
	return "", nil, errors.New("python@3 not registered")
}
