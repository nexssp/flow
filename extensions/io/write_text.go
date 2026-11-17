package io

import (
	"context"
	stdio "io"
	"os"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// WriteTextAction writes already-rendered text to stdout and passes the input through.
var WriteTextAction = action.New("io.write_text", func(_ context.Context, input map[string]any) (map[string]any, error) {
	return writeTextInput(os.Stdout, input)
}).
	Description("Write raw text to stdout and pass the input through").
	Tag("io", "stdout", "text").
	Build()

func writeTextInput(w stdio.Writer, input map[string]any) (map[string]any, error) {
	text, ok := input["text"].(string)
	if !ok {
		return nil, xerr.BadRequest("io.write_text: text must be a string")
	}
	if err := writeText(w, text); err != nil {
		return nil, xerr.Internal("io.write_text: could not write stdout", err)
	}
	return input, nil
}

func writeText(w stdio.Writer, text string) error {
	if text == "" {
		return nil
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	n, err := stdio.WriteString(w, text)
	if err != nil {
		return err
	}
	if n != len(text) {
		return stdio.ErrShortWrite
	}
	return nil
}
