package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nexssp/kernel/action"
)

// Debug prints the input as JSON to stderr and returns it unchanged.
// The optional @{ label: "..." } arg prefixes the printed line.
var Debug = action.New("runtime.debug", func(_ context.Context, in any) (any, error) {
	label := readStringArg(in, "label")
	suffix := ""
	if label != "" {
		suffix = " " + label
	}

	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[debug%s] <unprintable %T: %v>\n", suffix, in, err)
		return in, nil
	}
	fmt.Fprintf(os.Stderr, "[debug%s] %s\n", suffix, string(data))
	return in, nil
}).Description("Print input as JSON to stderr, pass through unchanged").
	Tag("base", "debug").
	Build()
