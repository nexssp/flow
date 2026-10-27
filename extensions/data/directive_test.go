package data

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

func TestHandleDataDescribesSourceWithoutParsingRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "orders.jsonl")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("not valid json yet\n"), 0o600))

	lines := []string{`@data orders :from="orders.jsonl"`}
	out := make(map[string]any)
	result, err := handleData(context.Background(), compiler.DirectiveReq{Lines: lines, I: 0, Out: out, File: filepath.Join(dir, "flow.nflow"), BaseDir: dir})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, result.Next, 1)
	ktest.RequireEqual(t, result.BlankLines, []int{0})

	datasets, ok := out[datasetMetaKey].(map[string]Dataset)
	ktest.RequireCondition(t, ok, "datasets metadata has type %T", out[datasetMetaKey])
	ktest.RequireEqual(t, len(datasets), 1)
	ktest.RequireEqual(t, datasets["orders"], Dataset{Name: "orders", Source: path})

	_, streamErr := collectStream(streamJSONL(context.Background(), path))
	ktest.RequireErrorContains(t, streamErr, ":1: invalid JSON object")
}

func TestHandleDataRejectsMissingAndDirectorySources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, test := range []struct {
		name string
		from string
		want string
	}{
		{name: "missing", from: "absent.jsonl", want: "stat"},
		{name: "directory", from: ".", want: "not a regular file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			line := `@data orders :from="` + test.from + `"`
			_, err := handleData(context.Background(), compiler.DirectiveReq{Lines: []string{line}, I: 0, Out: map[string]any{}, BaseDir: dir})
			ktest.RequireErrorContains(t, err, test.want)
		})
	}
}

func TestHandleDataRejectsSymlinkSources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "orders.jsonl")
	link := filepath.Join(dir, "linked-orders.jsonl")
	ktest.RequireNoError(t, os.WriteFile(target, []byte("{}\n"), 0o600))
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink in test environment: %v", err)
	}
	line := `@data orders :from="linked-orders.jsonl"`
	_, err := handleData(context.Background(), compiler.DirectiveReq{Lines: []string{line}, I: 0, Out: map[string]any{}, BaseDir: dir})
	ktest.RequireErrorContains(t, err, "not a regular file")
}

func TestHandleDataUseRewritesToDatasetSource(t *testing.T) {
	t.Parallel()
	body := []string{"@datause orders"}
	out := map[string]any{datasetMetaKey: map[string]Dataset{"orders": {Name: "orders", Source: "/tmp/orders.jsonl"}}}
	result, err := handleDataUse(context.Background(), compiler.DirectiveReq{Lines: body, Body: body, I: 0, Out: out})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, result.Next, 1)
	ktest.RequireEqual(t, body[0], `data.rows:name="orders"`)
}

func TestHandleDataRejectsUnknownOptionAndDuplicateDataset(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		line string
		out  map[string]any
		want string
	}{
		{name: "unknown option", line: `@data orders :path="orders.jsonl"`, want: "unknown option"},
		{name: "duplicate name", line: `@data orders :from="orders.jsonl"`, out: map[string]any{datasetMetaKey: map[string]Dataset{"orders": {Name: "orders"}}}, want: "already declared"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := handleData(context.Background(), compiler.DirectiveReq{Lines: []string{test.line}, I: 0, Out: test.out, BaseDir: t.TempDir()})
			ktest.RequireErrorContains(t, err, test.want)
		})
	}
}

func TestHandleDataUseRequiresPriorDeclaration(t *testing.T) {
	t.Parallel()
	body := []string{"@datause missing"}
	_, err := handleDataUse(context.Background(), compiler.DirectiveReq{Lines: body, Body: body, I: 0, Out: map[string]any{}})
	ktest.RequireErrorContains(t, err, "not declared earlier")
}

func TestInheritDatasetScopeCopiesParentDeclarations(t *testing.T) {
	t.Parallel()
	parentData := map[string]Dataset{"orders": {Name: "orders", Source: "/tmp/orders.jsonl"}}
	parent := map[string]any{datasetMetaKey: parentData}
	child := make(map[string]any)
	ktest.RequireNoError(t, inheritDatasetScope(parent, child))
	childData, ok := child[datasetMetaKey].(map[string]Dataset)
	ktest.RequireCondition(t, ok, "child datasets have type %T", child[datasetMetaKey])
	ktest.RequireEqual(t, childData["orders"], parentData["orders"])
	childData["local"] = Dataset{Name: "local", Source: "/tmp/local.jsonl"}
	ktest.RequireEqual(t, len(parentData), 1)
}

func TestInheritDatasetScopeRejectsShadowing(t *testing.T) {
	t.Parallel()
	parent := map[string]any{datasetMetaKey: map[string]Dataset{"orders": {Name: "orders"}}}
	child := map[string]any{datasetMetaKey: map[string]Dataset{"orders": {Name: "orders", Source: "child.jsonl"}}}
	ktest.RequireErrorContains(t, inheritDatasetScope(parent, child), "already declared")
}

func TestStreamJSONLReadsRowsIncrementallyAndSkipsBlankLines(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rows.jsonl")
	content := "{\"id\":\"a\"}\n\n{\"id\":\"b\"}\n"
	ktest.RequireNoError(t, os.WriteFile(path, []byte(content), 0o600))
	rows, err := collectStream(streamJSONL(context.Background(), path))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(rows), 2)
	first, ok := rows[0].(map[string]any)
	ktest.RequireCondition(t, ok, "first row has type %T", rows[0])
	second, ok := rows[1].(map[string]any)
	ktest.RequireCondition(t, ok, "second row has type %T", rows[1])
	ktest.RequireEqual(t, first["id"], "a")
	ktest.RequireEqual(t, second["id"], "b")
}

func TestStreamJSONLReportsMissingAndInvalidSources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	invalidPath := filepath.Join(dir, "invalid.jsonl")
	ktest.RequireNoError(t, os.WriteFile(invalidPath, []byte("{\"id\":1}\n\n{bad json}\n"), 0o600))

	_, missingErr := collectStream(streamJSONL(context.Background(), filepath.Join(dir, "missing.jsonl")))
	ktest.RequireErrorContains(t, missingErr, "open")

	_, invalidErr := collectStream(streamJSONL(context.Background(), invalidPath))
	ktest.RequireErrorContains(t, invalidErr, invalidPath+":3: invalid JSON object")
}

func TestStreamJSONLRejectsOversizedRecords(t *testing.T) {
	t.Parallel()
	for _, extraBytes := range []int{1, 3} {
		t.Run(fmt.Sprintf("extra-%d", extraBytes), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "oversized.jsonl")
			content := strings.Repeat("x", maxJSONLRecordSize+extraBytes)
			ktest.RequireNoError(t, os.WriteFile(path, []byte(content), 0o600))
			_, err := collectStream(streamJSONL(context.Background(), path))
			ktest.RequireErrorContains(t, err, path+":1: JSONL record exceeds")
			ktest.RequireCondition(t, !strings.Contains(err.Error(), "bufio.Scanner"), "scanner implementation leaked in error: %v", err)
		})
	}
}

func TestStreamJSONLStopsWithCancellation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rows.jsonl")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("{\"id\":1}\n{\"id\":2}\n"), 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source, err := streamJSONL(ctx, path)
	ktest.RequireNoError(t, err)

	rows := 0
	var streamErr error
	for _, rowErr := range source {
		if rowErr != nil {
			streamErr = rowErr
			break
		}
		rows++
		cancel()
	}
	ktest.RequireEqual(t, rows, 1)
	ktest.RequireCondition(t, errors.Is(streamErr, context.Canceled), "stream error = %v, want context.Canceled", streamErr)
}

func TestStreamJSONLRunScopeClosesFileOnCancellation(t *testing.T) {
	t.Parallel()
	ctx, scope := compiler.NewRunScope(context.Background())
	file := &trackedReadCloser{reader: strings.NewReader("{\"id\":1}\n{\"id\":2}\n")}
	source, err := streamJSONLFromFile(ctx, "rows.jsonl", file)
	ktest.RequireNoError(t, err)

	rows := 0
	var streamErr error
	for _, rowErr := range source {
		if rowErr != nil {
			streamErr = rowErr
			break
		}
		rows++
		if err := scope.Close(ctx); err != nil {
			t.Fatalf("close run scope: %v", err)
		}
		ktest.RequireCondition(t, file.closed, "RunScope did not close the active JSONL file")
	}
	ktest.RequireEqual(t, rows, 1)
	ktest.RequireCondition(t, errors.Is(streamErr, context.Canceled), "stream error = %v, want context.Canceled", streamErr)
	ktest.RequireEqual(t, file.closeCalls, 1)
}

func TestSplitDirectiveFieldsKeepsQuotedSpaces(t *testing.T) {
	t.Parallel()
	got, err := splitDirectiveFields(`orders :from="directory/order cases.jsonl"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(got), 2)
	ktest.RequireEqual(t, got[1], `:from="directory/order cases.jsonl"`)
	_, err = splitDirectiveFields(`orders :from="unterminated`)
	ktest.RequireCondition(t, err != nil && strings.Contains(err.Error(), "unterminated"), "expected unterminated quote error, got %v", err)
}

func collectStream(source iter.Seq2[any, error], err error) ([]any, error) {
	if err != nil {
		return nil, err
	}
	return collect(source)
}

type trackedReadCloser struct {
	reader     *strings.Reader
	closed     bool
	closeCalls int
}

func (f *trackedReadCloser) Read(p []byte) (int, error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	return f.reader.Read(p)
}

func (f *trackedReadCloser) Close() error {
	f.closed = true
	f.closeCalls++
	return nil
}

var _ io.ReadCloser = (*trackedReadCloser)(nil)
