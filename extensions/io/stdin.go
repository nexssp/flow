package io

import (
	"bufio"
	"context"
	"encoding/json"
	stdIo "io"
	"iter"
	"os"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

const (
	scanInitialBuffer = 64 * 1024
	scanMaxLineBytes  = 16 * 1024 * 1024
)

// InConfig controls io.stdin's per-line decoding.
type InConfig struct {
	// Format selects the decoder for each line. "lines" (default) yields
	// raw strings; "ndjson" parses one JSON value per line.
	Format string `json:"format" cli:"format"`
}

// InSource returns the io.stdin stream source reading process stdin.
func InSource() action.AnyStreamAction {
	return inFrom(os.Stdin)
}

// inFrom builds the io.stdin source over reader. Tests use it to avoid
// touching process stdin. Returns the concrete type so callers (tests,
// internal helpers) can use the typed Do method without a cast.
func inFrom(reader stdIo.Reader) *action.StreamAction[InConfig, any] {
	return action.NewStream("io.stdin", func(ctx context.Context, cfg InConfig) (iter.Seq2[any, error], error) {
		format := cfg.Format
		if format == "" {
			format = "lines"
		}
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, scanInitialBuffer), scanMaxLineBytes)
		switch format {
		case "lines":
			return scanLines(ctx, scanner), nil
		case "ndjson":
			return scanNDJSON(ctx, scanner), nil
		default:
			return nil, xerr.BadRequest("io.stdin: unknown format " + cfg.Format)
		}
	})
}

func scanLines(ctx context.Context, scanner *bufio.Scanner) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}
			if !yield(scanner.Text(), nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			yield(nil, xerr.Internal("io.stdin: read", err))
		}
	}
}

func scanNDJSON(ctx context.Context, scanner *bufio.Scanner) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var parsed any
			if err := json.Unmarshal(line, &parsed); err != nil {
				yield(nil, xerr.Validation("io.stdin: invalid ndjson line: "+err.Error()))
				return
			}
			if !yield(parsed, nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			yield(nil, xerr.Internal("io.stdin: read", err))
		}
	}
}
