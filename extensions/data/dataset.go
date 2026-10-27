package data

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"os"
	"strings"
	"sync"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

const (
	datasetMetaKey     = "data.datasets"
	maxJSONLRecordSize = 16 << 20
)

// Dataset describes a JSONL source during preprocessing. Rows are decoded
// incrementally when the compiled source executes.
type Dataset struct {
	Name   string
	Source string
}

type rowsConfig struct {
	Name string `json:"name" validate:"required"`
}

var datasetsKey = xctx.NewKey[map[string]Dataset]("flow.data.datasets")

// wrapPipeline installs the declared source descriptors for this pipeline
// invocation. Its backing JSONL files are not opened until data.rows executes.
func wrapPipeline(meta map[string]any, inner action.AnyAction) (action.AnyAction, error) {
	datasets, _ := meta[datasetMetaKey].(map[string]Dataset)
	if len(datasets) == 0 {
		return inner, nil
	}
	captured := maps.Clone(datasets)
	return action.New("data.pipeline", func(ctx context.Context, req any) (any, error) {
		ctx = datasetsKey.With(ctx, captured)
		return action.InvokeAny(ctx, inner, req)
	}).Build(), nil
}

// RowsSource exposes a declared dataset as a fresh, repeatable JSONL stream.
func RowsSource() action.AnyStreamAction {
	return action.NewStream("data.rows", func(ctx context.Context, cfg rowsConfig) (iter.Seq2[any, error], error) {
		datasets, ok := datasetsKey.From(ctx)
		if !ok {
			return nil, xerr.NotFound("data.rows: no @data declarations are available")
		}
		dataset, ok := datasets[cfg.Name]
		if !ok {
			return nil, xerr.NotFound("data.rows: dataset " + cfg.Name + " is not declared")
		}
		return streamJSONL(ctx, dataset.Source)
	})
}

func streamJSONL(ctx context.Context, path string) (iter.Seq2[any, error], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	return streamJSONLFromFile(ctx, path, file)
}

func streamJSONLFromFile(ctx context.Context, path string, file io.ReadCloser) (iter.Seq2[any, error], error) {
	var closeOnce sync.Once
	var closeErr error
	closeFile := func() error {
		closeOnce.Do(func() { closeErr = file.Close() })
		return closeErr
	}
	var release func()
	var err error
	if scope, ok := compiler.RunScopeFromContext(ctx); ok {
		release, err = scope.Register(func(context.Context) error { return closeFile() })
		if err != nil {
			return nil, errors.Join(err, closeFile())
		}
	}

	return func(yield func(any, error) bool) {
		defer func() {
			_ = closeFile() //nolint:errcheck // Early stop cannot accept another stream error; normal close errors are yielded below.
			if release != nil {
				release()
			}
		}()

		scanner := bufio.NewScanner(file)
		// Leave room for CRLF while enforcing the documented limit on the
		// record bytes returned by ScanLines.
		scanner.Buffer(make([]byte, 64*1024), maxJSONLRecordSize+2)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}
			if len(scanner.Bytes()) > maxJSONLRecordSize {
				yield(nil, fmt.Errorf("%s:%d: JSONL record exceeds %d bytes", path, lineNumber, maxJSONLRecordSize))
				return
			}
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				yield(nil, fmt.Errorf("%s:%d: invalid JSON object: %w", path, lineNumber, err))
				return
			}
			if row == nil {
				yield(nil, fmt.Errorf("%s:%d: each JSONL record must be a JSON object", path, lineNumber))
				return
			}
			if !yield(row, nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				yield(nil, ctxErr)
				return
			}
			if errors.Is(err, bufio.ErrTooLong) {
				yield(nil, fmt.Errorf("%s:%d: JSONL record exceeds %d bytes", path, lineNumber+1, maxJSONLRecordSize))
				return
			}
			yield(nil, fmt.Errorf("%s:%d: read JSONL: %w", path, lineNumber+1, err))
			return
		}
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		if err := closeFile(); err != nil {
			yield(nil, fmt.Errorf("close %q: %w", path, err))
		}
	}, nil
}

func inheritDatasetScope(parentMeta, childMeta map[string]any) error {
	parent, _ := parentMeta[datasetMetaKey].(map[string]Dataset)
	if len(parent) == 0 {
		return nil
	}
	child, _ := childMeta[datasetMetaKey].(map[string]Dataset)
	merged := make(map[string]Dataset, len(parent)+len(child))
	maps.Copy(merged, parent)
	for name, dataset := range child {
		if _, exists := merged[name]; exists {
			return fmt.Errorf("data: dataset %q is already declared in an inherited scope", name)
		}
		merged[name] = dataset
	}
	childMeta[datasetMetaKey] = merged
	return nil
}
