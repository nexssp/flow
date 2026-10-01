package fs

import (
	"bufio"
	"fmt"
	"iter"
	"os"

	"github.com/nexssp/kernel/action"
)

// Stdout prints each file's RelPath to stdout and passes the item
// through. It is the file-listing sink for `fs.walk -> out.stdout`.
func Stdout(up iter.Seq2[FileMeta, error]) iter.Seq2[FileMeta, error] {
	return func(yield func(FileMeta, error) bool) {
		writer := bufio.NewWriter(os.Stdout)
		defer writer.Flush()
		for item, err := range up {
			if err != nil {
				yield(FileMeta{}, err)
				return
			}
			fmt.Fprintln(writer, item.RelPath)
			_ = writer.Flush()
			if !yield(item, nil) {
				return
			}
		}
	}
}

func StdoutOperator() action.NamedOperator {
	return action.NewSimpleOperator("out.stdout", Stdout)
}
