package fsio

import (
	"bufio"
	"fmt"
	"iter"
	"os"
	"path/filepath"

	"github.com/nexssp/kernel/action"
)

type OutFileConfig struct {
	Path      string `json:"path" cli:"o,output"`
	Append    bool   `json:"append" cli:"append"`
	FlushEach bool   `json:"flush_each"` // Flushes per item, default false (flush at the end)
}

func OutFile[T any](cfg OutFileConfig) action.StreamOp[T, T] {
	return func(up iter.Seq2[T, error]) iter.Seq2[T, error] {
		return func(yield func(T, error) bool) {
			if cfg.Path == "" || cfg.Path == "-" {
				// Pass-through without writing if no path is provided or targeting stdout
				for item, err := range up {
					if !yield(item, err) {
						return
					}
				}
				return
			}

			if dir := filepath.Dir(cfg.Path); dir != "." && dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					var zero T
					yield(zero, fmt.Errorf("out.file: mkdir: %w", err))
					return
				}
			}

			flags := os.O_CREATE | os.O_WRONLY
			if cfg.Append {
				flags |= os.O_APPEND
			} else {
				flags |= os.O_TRUNC
			}

			f, err := os.OpenFile(cfg.Path, flags, 0o644)
			if err != nil {
				var zero T
				yield(zero, fmt.Errorf("out.file: open: %w", err))
				return
			}

			// We wrap the iteration in a closure so we can safely use defer for cleanup,
			// guaranteeing the file flushes and closes even if the stream consumer aborts early.
			err = func() (retErr error) {
				w := bufio.NewWriterSize(f, 64*1024)
				defer func() {
					if flushErr := w.Flush(); flushErr != nil && retErr == nil {
						retErr = fmt.Errorf("out.file: flush: %w", flushErr)
					}
					f.Close()
				}()

				for item, itemErr := range up {
					if itemErr != nil {
						return itemErr
					}

					var data []byte
					switch v := any(item).(type) {
					case FileContent:
						data = v.Content
					case []byte:
						data = v
					case string:
						data = []byte(v)
					}

					if len(data) > 0 {
						if _, wErr := w.Write(data); wErr != nil {
							return fmt.Errorf("out.file: write: %w", wErr)
						}
						if cfg.FlushEach {
							if fErr := w.Flush(); fErr != nil {
								return fmt.Errorf("out.file: flush: %w", fErr)
							}
						}
					}

					if !yield(item, nil) {
						return nil // Downstream stopped consuming
					}
				}
				return nil
			}()

			if err != nil {
				var zero T
				yield(zero, err)
			}
		}
	}
}

func OutFileOperator() action.NamedOperator {
	return action.NewOperator("out.file", OutFile[FileContent])
}
