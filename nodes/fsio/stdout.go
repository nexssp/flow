package fsio

import (
	"bufio"
	"fmt"
	"iter"
	"os"

	"github.com/nexssp/kernel/action"
)

func Stdout(up iter.Seq2[FileMeta, error]) iter.Seq2[FileMeta, error] {
	return func(yield func(FileMeta, error) bool) {
		w := bufio.NewWriter(os.Stdout)
		defer w.Flush()
		for item, err := range up {
			if err != nil {
				var zero FileMeta
				yield(zero, err)
				return
			}
			fmt.Fprintln(w, item.RelPath)
			_ = w.Flush()
			if !yield(item, nil) {
				return
			}
		}
	}
}

func StdoutOperator() action.NamedOperator {
	return action.NewSimpleOperator("out.stdout", Stdout)
}
