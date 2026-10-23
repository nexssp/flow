package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestEnvReport(t *testing.T) {
	//nolint:gosec // G101: deliberately fake credentials test that no toolchain or application secrets are read.
	values := map[string]string{
		"NFLOW_GO_VERBOSE":    "1",
		"NFLOW_GO_TIMEOUT":    "",
		"NFLOW_HARNESS_CACHE": "/tmp/with space\n\x1b[31m",
		"APP_SECRET":          "application-secret-must-not-leak",
		"GOPROXY":             "https://user:proxy-secret@example.invalid",
		"GOAUTH":              "authentication-secret-must-not-leak",
		"GOFLAGS":             "-ldflags=-X=main.token=flag-secret",
		"HTTP_PROXY":          "http://user:http-secret@example.invalid",
		"HTTPS_PROXY":         "",
	}
	seen := make(map[string]bool)
	var out bytes.Buffer
	printEnv(&out, func(name string) (string, bool) {
		ktest.RequireCondition(t, !seen[name], "duplicate inventory entry: %s", name)
		seen[name] = true
		value, ok := values[name]
		return value, ok
	})
	text := out.String()
	for _, line := range []string{
		`NFLOW_GO_VERBOSE       set   "1"`,
		`NFLOW_GO_TIMEOUT       set   ""`,
		`NFLOW_HARNESS_CACHE    set   "/tmp/with space\n\x1b[31m"`,
		`NFLOW_HARNESS          unset -`,
		"default: 10m",
		"Internal child-runner marker",
		"NEXSS FLOW · ENV",
		"NFlow configuration",
		"Standard variables also read directly by NFlow",
	} {
		ktest.RequireStringContains(t, text, line)
	}
	for _, name := range []string{
		"APP_SECRET", "GOPROXY", "GOAUTH", "GOFLAGS", "GOOS", "GOARCH", "CGO_ENABLED",
		"XDG_CACHE_HOME", "HOME", "LOCALAPPDATA", "TMPDIR", "USERPROFILE", "home",
		"PATH", "PATHEXT", "NoDefaultCurrentDirectoryInExePath", "SYSTEMROOT",
		"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD",
	} {
		ktest.RequireEqual(t, false, seen[name])
		ktest.RequireEqual(t, false, strings.Contains(text, "  "+name+" "))
		if value := values[name]; value != "" {
			ktest.RequireEqual(t, false, strings.Contains(text, value))
		}
	}
	ktest.RequireEqual(t, false, strings.ContainsRune(text, '\x1b'))
	ktest.RequireEqual(t, 2, len(envGroups))
	ktest.RequireEqual(t, 12, len(seen))
	for _, name := range []string{
		"NFLOW_GO_VERBOSE", "NFLOW_GO_TIMEOUT", "NFLOW_HARNESS_CACHE", "NFLOW_HARNESS",
		"LOG_LEVEL", "LOG_FORMAT", "NO_COLOR", "GOBIN", "GOPATH", "GOWORK", "TEMP", "TMP",
	} {
		ktest.RequireEqual(t, true, seen[name])
	}
	for _, group := range envGroups {
		ktest.RequireCondition(t, group.title != "", "missing environment group title")
		for _, variable := range group.vars {
			ktest.RequireCondition(t, variable.purpose != "", "missing description for %s", variable.name)
		}
	}
}

func TestEnvCLI(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"report", []string{"env"}, 0, "NFlow environment"},
		{"help command", []string{"help", "env"}, 0, "nflow env"},
		{"help flag", []string{"env", "--help"}, 0, "nflow env"},
		{"short help flag", []string{"env", "-h"}, 0, "nflow env"},
		{"help word", []string{"env", "help"}, 0, "nflow env"},
		{"no arbitrary lookup", []string{"env", "APP_SECRET"}, 2, "usage: nflow env"},
		{"no unsupported flag", []string{"env", "--json"}, 2, "usage: nflow env"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr := captureIO(t, func() {
				ktest.RequireEqual(t, tc.code, runCLI(context.Background(), nil, tc.args))
			})
			if tc.code == 0 {
				ktest.RequireStringContains(t, stdout, tc.want)
				ktest.RequireEqual(t, "", stderr)
			} else {
				ktest.RequireStringContains(t, stderr, tc.want)
				ktest.RequireEqual(t, "", stdout)
			}
		})
	}
}

func TestEnvHelpAndCompletion(t *testing.T) {
	var out bytes.Buffer
	PrintTopHelp(&out)
	ktest.RequireStringContains(t, out.String(), "env          Show NFlow environment settings")
	for _, shell := range SupportedShells {
		out.Reset()
		ktest.RequireNoError(t, Completion(shell, &out))
		text := out.String()
		switch shell {
		case "bash":
			ktest.RequireStringContains(t, text, " info help version env\"")
		case "zsh":
			ktest.RequireStringContains(t, text, "'env:show NFlow environment settings'")
		default:
			ktest.RequireStringContains(t, text, "'version', 'env')")
		}
	}
}
