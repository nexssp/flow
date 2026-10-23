package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestVersionCheck(t *testing.T) {
	for _, tc := range []struct {
		name, current, latest, want string
	}{
		{"older", "v0.37.0", "v0.39.0", "go install github.com/nexssp/flow/cmd/nflow@v0.39.0"},
		{"equal", "0.39.0", "v0.39.0", "Already up to date."},
		{"newer", "v0.40.0", "v0.39.0", "newer than the latest stable release"},
		{"numeric ordering", "v0.9.0", "v0.10.0", "Update available."},
		{"prerelease", "v0.39.0-rc.1", "v0.39.0", "Update available."},
		{"build metadata", "v0.39.0+local", "v0.39.0", "Already up to date."},
		{"dev", "dev", "v0.39.0", "development or unknown build"},
		{"unknown", "unknown", "v0.39.0", "development or unknown build"},
		{"empty", "", "v0.39.0", "development or unknown build"},
		{"git describe", "v0.37.0-3-g123abc", "v0.39.0", "development or unknown build"},
		{"dev prerelease", "v0.37.0-dev", "v0.39.0", "development or unknown build"},
		{"pseudo version", "v0.37.1-0.20261022000000-abcdef123456", "v0.39.0", "development or unknown build"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			code := checkVersion(t.Context(), &out, tc.current, "", func(context.Context) (string, error) { return tc.latest, nil })
			ktest.RequireEqual(t, 0, code)
			ktest.RequireStringContains(t, out.String(), "Current: "+tc.current)
			ktest.RequireStringContains(t, out.String(), "Latest: "+tc.latest)
			ktest.RequireStringContains(t, out.String(), tc.want)
			if !strings.Contains(tc.want, "go install") && tc.want != "Update available." {
				ktest.RequireEqual(t, false, strings.Contains(out.String(), "go install"))
			}
		})
	}
}

func TestVersionCheckDirtyBuild(t *testing.T) {
	var out bytes.Buffer
	code := checkVersion(t.Context(), &out, "v0.37.0", "abcdef-dirty", func(context.Context) (string, error) { return "v0.39.0", nil })
	ktest.RequireEqual(t, code, 0)
	ktest.RequireStringContains(t, out.String(), "development or unknown build")
	ktest.RequireStringNotContains(t, out.String(), "Update available.")
	ktest.RequireStringNotContains(t, out.String(), "go install")
}

func TestVersionCheckLookupFailure(t *testing.T) {
	var out bytes.Buffer
	code := checkVersion(t.Context(), &out, "v0.37.0", "", func(context.Context) (string, error) { return "", errors.New("network offline") })
	ktest.RequireEqual(t, 1, code)
	ktest.RequireStringContains(t, out.String(), "Latest: unavailable")
	ktest.RequireStringContains(t, out.String(), "network offline")
	ktest.RequireStringContains(t, out.String(), "Update availability could not be determined.")
	ktest.RequireEqual(t, false, strings.Contains(out.String(), "Update available."))
	ktest.RequireEqual(t, false, strings.Contains(out.String(), "go install"))
}

type versionTransport func(*http.Request) (*http.Response, error)

func (f versionTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestFetchLatestRelease(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"stable", `{"tag_name":"v0.39.0","draft":false,"prerelease":false}`, "v0.39.0", 200},
		{"without prefix", `{"tag_name":"0.39.0"}`, "v0.39.0", 200},
		{"rate limit", `{}`, "", 403},
		{"no release", `{}`, "", 404},
		{"server error", `{}`, "", 500},
		{"malformed", `{`, "", 200},
		{"empty", `{}`, "", 200},
		{"draft", `{"tag_name":"v0.39.0","draft":true}`, "", 200},
		{"prerelease", `{"tag_name":"v0.39.0","prerelease":true}`, "", 200},
		{"prerelease tag", `{"tag_name":"v0.39.0-rc.1"}`, "", 200},
		{"injection", `{"tag_name":"v0.39.0;echo bad"}`, "", 200},
		{"short version", `{"tag_name":"v0.39"}`, "", 200},
		{"wrong field type", `{"tag_name":39}`, "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: versionTransport(func(req *http.Request) (*http.Response, error) {
				ktest.RequireEqual(t, latestReleaseURL, req.URL.String())
				ktest.RequireEqual(t, http.MethodGet, req.Method)
				ktest.RequireEqual(t, "application/vnd.github+json", req.Header.Get("Accept"))
				ktest.RequireEqual(t, "nflow-version-check", req.Header.Get("User-Agent"))
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			got, err := fetchLatestRelease(t.Context(), client)
			if tc.want == "" {
				ktest.RequireCondition(t, err != nil, "expected lookup failure")
			} else {
				ktest.RequireNoError(t, err)
			}
			ktest.RequireEqual(t, tc.want, got)
		})
	}
}

func TestFetchLatestReleaseCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := &http.Client{Transport: versionTransport(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})}
	_, err := fetchLatestRelease(ctx, client)
	ktest.RequireCondition(t, err != nil, "expected lookup failure")
	ktest.RequireEqual(t, true, errors.Is(err, context.Canceled))
}

func TestVersionOfflineDispatchAndHelp(t *testing.T) {
	previous := http.DefaultTransport
	http.DefaultTransport = versionTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("offline version/help must not access the network")
		return nil, errors.New("unexpected network")
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}, {"version", "--help"}, {"version", "check", "--help"}, {"help", "version"}} {
		ktest.RequireEqual(t, 0, RunContext(t.Context(), args))
	}
	ktest.RequireEqual(t, 2, RunContext(t.Context(), []string{"version", "unknown"}))
	ktest.RequireEqual(t, 2, RunContext(t.Context(), []string{"version", "check", "extra"}))
	var help bytes.Buffer
	ktest.RequireEqual(t, true, PrintCommandHelp(&help, "version"))
	ktest.RequireStringContains(t, help.String(), "nflow version [check]")
	ktest.RequireStringContains(t, help.String(), "without updating")
}

func TestVersionCheckDispatch(t *testing.T) {
	previous := http.DefaultTransport
	called := false
	http.DefaultTransport = versionTransport(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v0.39.0"}`)), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	ktest.RequireEqual(t, 0, RunContext(t.Context(), []string{"version", "check"}))
	ktest.RequireEqual(t, true, called)
}

func TestVersionCheckCompletion(t *testing.T) {
	//nolint:gosec // G101: shell completion snippets, not credentials.
	for shell, want := range map[string]string{
		"bash": `compgen -W "check"`,
		"zsh":  `_values 'operation' check`,
		"pwsh": `emit @('check') 'ParameterValue'`,
	} {
		var script bytes.Buffer
		ktest.RequireNoError(t, Completion(shell, &script))
		ktest.RequireStringContains(t, script.String(), want)
	}
}
