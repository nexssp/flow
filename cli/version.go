package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nexssp/kernel/xerr"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

const latestReleaseURL = "https://api.github.com/repos/nexssp/flow/releases/latest"

var gitDescribeVersion = regexp.MustCompile(`-\d+-g[0-9a-f]+(-dirty)?$`)

func runVersion(ctx context.Context, args []string) int {
	if wantsHelp(args) || (len(args) == 2 && args[0] == "check" && wantsHelp(args[1:])) {
		PrintCommandHelp(os.Stdout, "version")
		return 0
	}
	if len(args) == 0 {
		Print(os.Stdout)
		return 0
	}
	if len(args) != 1 || args[0] != "check" {
		return fatalf("usage: nflow version [check]")
	}
	current, commit, _ := resolvedBuildInfo()
	client := &http.Client{Timeout: 10 * time.Second}
	return checkVersion(ctx, os.Stdout, current, commit, func(ctx context.Context) (string, error) {
		return fetchLatestRelease(ctx, client)
	})
}

func checkVersion(ctx context.Context, out io.Writer, current, commit string, lookup func(context.Context) (string, error)) int {
	fmt.Fprintf(out, "Current: %s\n", current)
	latest, err := lookup(ctx)
	if err != nil {
		fmt.Fprintf(out, "Latest: unavailable\nVersion check failed: %v\nUpdate availability could not be determined.\n", err)
		return 1
	}
	fmt.Fprintf(out, "Latest: %s\n", latest)
	installed := releaseVersion(current)
	if strings.HasSuffix(commit, "-dirty") || installed == "" || module.IsPseudoVersion(installed) || gitDescribeVersion.MatchString(installed) ||
		strings.HasSuffix(installed, "-dirty") || strings.Contains(strings.ToLower(semver.Prerelease(installed)), "dev") {
		fmt.Fprintln(out, "Current version is a development or unknown build; update availability could not be determined.")
		return 0
	}
	switch semver.Compare(installed, latest) {
	case -1:
		fmt.Fprintln(out, "Update available. To update manually (requires Go):")
		fmt.Fprintf(out, "  go install github.com/nexssp/flow/cmd/nflow@%s\n", latest)
	case 0:
		fmt.Fprintln(out, "Already up to date.")
	case 1:
		fmt.Fprintln(out, "Current version is newer than the latest stable release; no update suggested.")
	}
	return 0
}

// releaseVersion accepts full semantic versions, with or without a v prefix.
// Unlike the DSL's compatibility gate, release checks preserve prerelease ordering.
func releaseVersion(raw string) string {
	v := "v" + strings.TrimPrefix(raw, "v")
	if !semver.IsValid(v) || semver.MajorMinor(v) == v || semver.Major(v) == v {
		return ""
	}
	return v
}

func fetchLatestRelease(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, http.NoBody)
	if err != nil {
		return "", xerr.Internal("create release request", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "nflow-version-check")
	resp, err := client.Do(req)
	if err != nil {
		return "", xerr.Unavailable("lookup latest GitHub release", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", xerr.Unavailable(fmt.Sprintf("GitHub release lookup returned HTTP %d", resp.StatusCode))
	}
	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return "", xerr.Unavailable("decode latest GitHub release", err)
	}
	v := releaseVersion(release.TagName)
	if release.Draft || release.Prerelease || v == "" || semver.Prerelease(v) != "" {
		return "", xerr.Unavailable("GitHub did not return a stable semantic release tag")
	}
	return v, nil
}
