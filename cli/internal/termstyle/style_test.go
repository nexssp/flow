package termstyle

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestPalette(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paint func(Style, string) string
		code  string
	}{
		{"bold", Style.Bold, "1"},
		{"dim", Style.Dim, "2"},
		{"red", Style.Red, "31"},
		{"green", Style.Green, "32"},
		{"cyan", Style.Cyan, "36"},
		{"magenta", Style.Magenta, "35"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ktest.RequireEqual(t, "label", tc.paint(false, "label"))
			ktest.RequireEqual(t, "\x1b["+tc.code+"mlabel\x1b[0m", tc.paint(true, "label"))
		})
	}
}

func TestReportHierarchy(t *testing.T) {
	plain := Style(false)
	banner := plain.Banner("ENV", "current process")
	ktest.RequireStringContains(t, banner, "NEXSS FLOW · ENV")
	ktest.RequireStringContains(t, banner, "current process")
	ktest.RequireEqual(t, 2, strings.Count(banner, strings.Repeat("─", 70)))
	ktest.RequireEqual(t, "\n▸ NFlow configuration\n\n", plain.Section("NFlow configuration"))
	ktest.RequireEqual(t, false, strings.ContainsRune(banner, '\x1b'))
	ktest.RequireStringContains(t, Style(true).Banner("ENV", "current process"), "\x1b[35m")
	ktest.RequireStringContains(t, Style(true).Section("section"), "\x1b[36m▸\x1b[0m")
}

func TestNoninteractiveAndNoColor(t *testing.T) {
	for _, value := range []string{"", "1", "0"} {
		t.Run("NO_COLOR="+value, func(t *testing.T) {
			t.Setenv("NO_COLOR", value)
			ktest.RequireEqual(t, Style(false), New(&bytes.Buffer{}))
			f, err := os.CreateTemp(t.TempDir(), "redirected")
			ktest.RequireNoError(t, err)
			t.Cleanup(func() { _ = f.Close() })
			ktest.RequireEqual(t, Style(false), New(f))
			if value != "" {
				ktest.RequireEqual(t, Style(false), New(os.Stdout))
			}
		})
	}
}
