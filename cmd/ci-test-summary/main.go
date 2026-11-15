package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

type testKey struct {
	pkg  string
	test string
}

type failedTest struct {
	pkg     string
	test    string
	elapsed float64
}

type testReport struct {
	testOutput    map[testKey][]string
	packageOutput map[string][]string
	unknownOutput []string
	failedTests   []failedTest
	failedPackage map[string]bool
	packages      map[string]bool
	passedTests   int
	skippedTests  int
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/ci-test-summary")
		return 2
	}

	logPath := testLogPath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "could not create test log directory: %v\n", err)
		return 2
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create test log %q: %v\n", logPath, err)
		return 2
	}

	fmt.Fprintf(os.Stdout, "Running Go tests; detailed event output is being captured.\n")
	cmd := exec.CommandContext(context.Background(), "go", "test", "-json", "-count=1", "./builder", "./cli/...")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	runErr := cmd.Run()
	closeErr := logFile.Close()

	report, parseErr := parseTestEventsFile(logPath)
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "could not close test log: %v\n", closeErr)
		return 2
	}
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "could not parse test output: %v\n", parseErr)
		return 2
	}

	if runErr == nil {
		summary := fmt.Sprintf("Go tests passed: %d test(s) across %d package(s).", report.passedTests, len(report.packages))
		fmt.Fprintln(os.Stdout, summary)
		appendGitHubSummary("### Harness tests passed\n\n" + summary + "\n")
		return 0
	}

	exitCode := 1
	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
		exitCode = exitErr.ExitCode()
	}
	summary := renderFailureSummary(report, exitCode, logPath)
	fmt.Fprintln(os.Stderr, summary)
	failures := leafFailedTests(report.failedTests)
	failureCount := len(failures) + packageBuildFailureCount(failures, report.failedPackage)
	if failureCount == 0 {
		failureCount = 1
	}
	fmt.Fprintf(os.Stderr, "::error title=Go test failure::%d test/build failure(s) reported; see the step summary and full test event log.\n", failureCount)
	appendGitHubSummary(summary + "\n")
	return exitCode
}

func testLogPath() string {
	dir := os.Getenv("RUNNER_TEMP")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "flow-test-harness.jsonl")
}

func parseTestEventsFile(path string) (testReport, error) {
	file, err := os.Open(path)
	if err != nil {
		return testReport{}, err
	}
	defer file.Close()
	return parseTestEvents(file)
}

func parseTestEvents(input io.Reader) (testReport, error) {
	report := testReport{
		testOutput:    map[testKey][]string{},
		packageOutput: map[string][]string{},
		failedPackage: map[string]bool{},
		packages:      map[string]bool{},
	}

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		var event testEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			if strings.TrimSpace(line) != "" {
				report.unknownOutput = append(report.unknownOutput, line)
			}
			continue
		}
		if event.Package != "" {
			report.packages[event.Package] = true
		}
		if event.Action == "output" {
			if event.Test == "" {
				report.packageOutput[event.Package] = appendOutput(report.packageOutput[event.Package], event.Output)
			} else {
				key := testKey{pkg: event.Package, test: event.Test}
				report.testOutput[key] = appendOutput(report.testOutput[key], event.Output)
			}
			continue
		}
		if event.Test != "" {
			switch event.Action {
			case "pass":
				report.passedTests++
			case "skip":
				report.skippedTests++
			case "fail":
				report.failedTests = append(report.failedTests, failedTest{pkg: event.Package, test: event.Test, elapsed: event.Elapsed})
			}
			continue
		}
		if event.Action == "fail" && event.Package != "" {
			report.failedPackage[event.Package] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return report, err
	}
	return report, nil
}

func appendOutput(lines []string, output string) []string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func leafFailedTests(failures []failedTest) []failedTest {
	var leaves []failedTest
	for _, candidate := range failures {
		hasFailedChild := false
		for _, other := range failures {
			if candidate.pkg == other.pkg && candidate.test != other.test && strings.HasPrefix(other.test, candidate.test+"/") {
				hasFailedChild = true
				break
			}
		}
		if !hasFailedChild {
			leaves = append(leaves, candidate)
		}
	}
	sort.Slice(leaves, func(i, j int) bool {
		if leaves[i].pkg != leaves[j].pkg {
			return leaves[i].pkg < leaves[j].pkg
		}
		return leaves[i].test < leaves[j].test
	})
	return leaves
}

func renderFailureSummary(report testReport, exitCode int, logPath string) string {
	failures := leafFailedTests(report.failedTests)
	packageFailures := make([]string, 0, len(report.failedPackage))
	for pkg := range report.failedPackage {
		packageFailures = append(packageFailures, pkg)
	}
	sort.Strings(packageFailures)
	buildFailures := packageBuildFailureCount(failures, report.failedPackage)

	failedPackages := map[string]bool{}
	for _, failure := range failures {
		failedPackages[failure.pkg] = true
	}
	for _, pkg := range packageFailures {
		failedPackages[pkg] = true
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Go tests failed (exit %d): %d failing test(s), %d package/build failure(s) across %d package(s).", exitCode, len(failures), buildFailures, len(failedPackages)))

	for _, failure := range failures {
		lines = append(lines, fmt.Sprintf("\n- %s — %s (%s)", failure.pkg, failure.test, formatElapsed(failure.elapsed)))
		diagnostics := diagnosticsForTest(report, failure)
		for _, line := range diagnostics {
			lines = append(lines, "    "+line)
		}
	}

	for _, pkg := range packageFailures {
		if failedPackages[pkg] && hasFailedTestForPackage(failures, pkg) {
			continue
		}
		lines = append(lines, "\n- "+pkg+" — package/build failure")
		for _, line := range tailLines(report.packageOutput[pkg], 12) {
			lines = append(lines, "    "+truncate(line))
		}
	}

	if len(failures) == 0 && len(packageFailures) == 0 {
		lines = append(lines, "\nNo individual failing test was reported by go test.")
		for _, line := range tailLines(report.unknownOutput, 12) {
			lines = append(lines, "    "+truncate(line))
		}
	}
	logMessage := "Full machine-readable log: " + logPath
	if os.Getenv("RUNNER_TEMP") != "" {
		logMessage += " (uploaded as a workflow artifact)"
	}
	lines = append(lines, "\n"+logMessage+".")
	return strings.Join(lines, "\n")
}

func diagnosticsForTest(report testReport, failure failedTest) []string {
	var keys []testKey
	for key := range report.testOutput {
		if key.pkg == failure.pkg && (key.test == failure.test || strings.HasPrefix(key.test, failure.test+"/")) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].test < keys[j].test })
	var output []string
	for _, key := range keys {
		output = append(output, report.testOutput[key]...)
	}
	if summary := conformanceFailureSummary(output); len(summary) > 0 {
		return summary
	}
	return tailLines(output, 12)
}

func conformanceFailureSummary(output []string) []string {
	joined := strings.Join(output, "\n")
	marker := "NFlow conformance self-test failed"
	markerAt := strings.Index(joined, marker)
	if markerAt < 0 {
		return nil
	}
	jsonAt := strings.Index(joined[markerAt:], "{")
	if jsonAt < 0 {
		return nil
	}

	var report struct {
		Failed   int `json:"failed"`
		Sections []struct {
			Name     string `json:"name"`
			Features []struct {
				Name   string `json:"name"`
				Source string `json:"source"`
				Status string `json:"status"`
				Error  string `json:"error"`
			} `json:"features"`
		} `json:"sections"`
	}
	decoder := json.NewDecoder(strings.NewReader(joined[markerAt+jsonAt:]))
	if err := decoder.Decode(&report); err != nil || report.Failed == 0 {
		return nil
	}
	lines := []string{fmt.Sprintf("NFlow conformance self-test: %d feature(s) failed", report.Failed)}
	shown := 0
	for _, section := range report.Sections {
		for _, feature := range section.Features {
			if feature.Status != "failed" {
				continue
			}
			line := fmt.Sprintf("[%s] %s", section.Name, feature.Name)
			if feature.Source != "" {
				line += " — " + feature.Source
			}
			if feature.Error != "" {
				line += ": " + feature.Error
			}
			lines = append(lines, truncate(line))
			shown++
			if shown == 10 {
				break
			}
		}
		if shown == 10 {
			break
		}
	}
	if report.Failed > shown {
		lines = append(lines, fmt.Sprintf("... and %d more failed feature(s)", report.Failed-shown))
	}
	return lines
}

func tailLines(lines []string, limit int) []string {
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, truncate(line))
		}
	}
	return result
}

func hasFailedTestForPackage(failures []failedTest, pkg string) bool {
	for _, failure := range failures {
		if failure.pkg == pkg {
			return true
		}
	}
	return false
}

func packageBuildFailureCount(failures []failedTest, packages map[string]bool) int {
	count := 0
	for pkg := range packages {
		if !hasFailedTestForPackage(failures, pkg) {
			count++
		}
	}
	return count
}

func formatElapsed(seconds float64) string {
	if seconds <= 0 {
		return "time unavailable"
	}
	return fmt.Sprintf("%.2fs", seconds)
}

const maxSummaryLineLength = 500

func truncate(value string) string {
	if len(value) <= maxSummaryLineLength {
		return value
	}
	return value[:maxSummaryLineLength] + "…"
}

func appendGitHubSummary(markdown string) {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return
	}
	// #nosec G703 -- GitHub Actions supplies this per-step summary path.
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not append GitHub step summary: %v\n", err)
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintln(file, markdown)
}
