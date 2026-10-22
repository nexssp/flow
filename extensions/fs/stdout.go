package fs

import (
	"bufio"
	"fmt"
	"io"
	"iter"
	"os"

	"github.com/nexssp/kernel/action"
)

// Stdout prints each file's RelPath to stdout and passes the item
// through. It is the file-listing sink for `fs.walk -> out.stdout`.
func Stdout(up iter.Seq2[FileMeta, error]) iter.Seq2[FileMeta, error] {
	return func(yield func(FileMeta, error) bool) {
		stdoutTo(os.Stdout, up)(yield)
	}
}

func stdoutTo(output io.Writer, up iter.Seq2[FileMeta, error]) iter.Seq2[FileMeta, error] {
	return func(yield func(FileMeta, error) bool) {
		writer := bufio.NewWriter(output)
		for item, err := range up {
			if err != nil {
				yield(FileMeta{}, err)
				return
			}
			if _, err := fmt.Fprintln(writer, item.RelPath); err != nil {
				yield(FileMeta{}, err)
				return
			}
			if err := writer.Flush(); err != nil {
				yield(FileMeta{}, err)
				return
			}
			if !yield(item, nil) {
				return
			}
		}
	}
}

func StdoutOperator() action.NamedOperator {
	return action.NewSimpleOperator("out.stdout", Stdout)
}
