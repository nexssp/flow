package io

import (
	"bufio"
	"fmt"
	"io"
	"iter"
	"os"

	"github.com/nexssp/kernel/action"
)

// StdoutOperator returns io.stdout: each item is written as a line to
// stdout and passed through unchanged.
func StdoutOperator() action.NamedOperator {
	return action.NewSimpleOperator[any, any]("io.stdout",
		writeLineTo(func() io.Writer { return os.Stdout }))
}

// StderrOperator returns io.stderr: each item is written as a line to
// stderr and passed through unchanged.
func StderrOperator() action.NamedOperator {
	return action.NewSimpleOperator[any, any]("io.stderr",
		writeLineTo(func() io.Writer { return os.Stderr }))
}

// writeLineTo returns an operator writing each item to w as one line.
// On a character device (interactive terminal) every line is flushed
// immediately so downstream tools and the user see progress in real
// time; redirected output stays buffered.
func writeLineTo(get func() io.Writer) func(iter.Seq2[any, error]) iter.Seq2[any, error] {
	return func(up iter.Seq2[any, error]) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			w := get()
			writer := bufio.NewWriter(w)
			flushEach := isCharDevice(w)
			defer func() { _ = writer.Flush() }()
			for item, err := range up {
				if err != nil {
					yield(nil, err)
					return
				}
				if _, err := fmt.Fprintln(writer, item); err != nil {
					yield(nil, err)
					return
				}
				if flushEach {
					_ = writer.Flush()
				}
				if !yield(item, nil) {
					return
				}
			}
		}
	}
}

func isCharDevice(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
