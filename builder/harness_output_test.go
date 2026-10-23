package builder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestRunGoOutputContextDiagnostics(t *testing.T) {
	const modulePath = "example.invalid/quietresolver"
	const version = "v1.0.0"
	const resolverMod = "module example.invalid/resolvercheck\n\ngo 1.23\n"
	const warning = "quadratic time for specially crafted CDF files"
	proxy := filepath.Join(t.TempDir(), "proxy")
	for _, v := range []string{version, "v1.0.1"} {
		writeGoModuleProxy(t, proxy, modulePath, v, map[string]string{
			"library.go": "package quietresolver\n",
		})
	}
	retractedPath := modulePath + "/retracted"
	for _, v := range []string{version, "v1.0.1"} {
		writeGoModuleProxy(t, proxy, retractedPath, v, map[string]string{
			"library.go": "package retracted\n",
		})
	}
	versionDir := filepath.Join(proxy, filepath.FromSlash(retractedPath), "@v")
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(versionDir, "list"), []byte("v1.0.0\nv1.0.1\n"), 0o600))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(versionDir, "v1.0.1.mod"),
		[]byte("module "+retractedPath+"\n\ngo 1.23\n\nretract v1.0.0 // "+warning+"\n"), 0o600))
	t.Setenv("GOPROXY", localFileProxyURL(proxy)+",off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "-modcacherw")
	// Isolate module metadata so earlier runs cannot mask retraction lookups.
	t.Setenv("GOMODCACHE", t.TempDir())

	for _, tc := range []struct {
		name    string
		verbose string
		fail    bool
		warning bool
	}{
		{name: "quiet success"},
		{name: "verbose success", verbose: "1"},
		{name: "quiet failure", fail: true},
		{name: "verbose failure", verbose: "1", fail: true},
		{name: "quiet retraction", warning: true},
		{name: "verbose retraction", verbose: "1", warning: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NFLOW_GO_VERBOSE", tc.verbose)
			ctx := WithGoDiagnostics(t.Context())
			dir := t.TempDir()
			ktest.RequireNoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(resolverMod), 0o600))

			stderr, err := os.CreateTemp(t.TempDir(), "stderr")
			ktest.RequireNoError(t, err)
			previous := os.Stderr
			os.Stderr = stderr
			t.Cleanup(func() {
				os.Stderr = previous
				_ = stderr.Close()
			})

			path := modulePath
			if tc.warning {
				path = retractedPath
			}
			target := path + "@" + version
			if tc.fail {
				target = path + "@v9.9.9"
			}
			_, err = runGoOutputContext(ctx, dir, "get", target)
			if tc.fail {
				ktest.RequireErrorContains(t, err, "go get "+target)
				ktest.RequireErrorContains(t, err, path)
				ktest.RequireErrorContains(t, err, "module lookup disabled")
				err = runGoContext(ctx, dir, "get", target)
				ktest.RequireErrorContains(t, err, "go get "+target)
				ktest.RequireErrorContains(t, err, path)
				ktest.RequireErrorContains(t, err, "module lookup disabled")
			} else {
				ktest.RequireNoError(t, err)
				// Repeat with warm module caches, as happens before a runner cache hit.
				ktest.RequireNoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(resolverMod), 0o600))
				_, err = runGoOutputContext(ctx, dir, "get", target)
				ktest.RequireNoError(t, err)
				// A third resolver/build step shares the invocation warning scope.
				ktest.RequireNoError(t, runGoContext(ctx, dir, "get", target))
				stdout, err := runGoOutputContext(ctx, dir, "list", "-json", path)
				ktest.RequireNoError(t, err)
				var pkg struct {
					ImportPath string `json:"ImportPath"`
				}
				ktest.RequireNoError(t, json.Unmarshal(stdout, &pkg))
				ktest.RequireEqual(t, pkg.ImportPath, path)
				if !tc.warning {
					for _, selector := range []string{"v1.0.1", version, "none"} {
						_, err = runGoOutputContext(ctx, dir, "get", path+"@"+selector)
						ktest.RequireNoError(t, err)
					}
				}
			}

			output, err := os.ReadFile(stderr.Name())
			ktest.RequireNoError(t, err)
			text := string(output)
			if tc.warning && tc.verbose == "1" {
				ktest.RequireEqual(t, strings.Count(text, "go: warning: "+path+"@"+version), 3)
				ktest.RequireStringContains(t, text, "go: warning: "+path+"@"+version+": retracted by module author: "+warning)
				ktest.RequireStringContains(t, text, "go: to switch to the latest unretracted version, run:")
				ktest.RequireStringContains(t, text, "go get "+path+"@latest")
			}
			if tc.verbose == "1" {
				ktest.RequireStringContains(t, text, path)
				if !tc.fail {
					ktest.RequireStringContains(t, text, "go: added "+path+" "+version)
				}
				if !tc.fail && !tc.warning {
					for _, verb := range []string{"upgraded", "downgraded", "removed"} {
						ktest.RequireStringContains(t, text, "go: "+verb+" "+path+" ")
					}
				}
			} else {
				for _, verb := range []string{"added", "upgraded", "downgraded", "removed"} {
					ktest.RequireCondition(t, !strings.Contains(text, "go: "+verb+" "), "routine %s output leaked: %s", verb, text)
				}
				if tc.warning {
					ktest.RequireEqual(t, text, "nflow: warning: "+path+"@"+version+" is retracted: "+warning+"\n")
				} else {
					ktest.RequireEqual(t, text, "")
				}
			}
		})
	}
}
