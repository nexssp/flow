package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/action"
)

const (
	printDefaultLimit   = 25
	printDefaultDepth   = 4
	printDefaultStrings = 200
	printMaxBytes       = 16 * 1024

	printReset   = "\x1b[0m"
	printDim     = "\x1b[2m"
	printCyan    = "\x1b[36m"
	printGreen   = "\x1b[32m"
	printYellow  = "\x1b[33m"
	printMagenta = "\x1b[35m"
)

var printConfigKeys = map[string]struct{}{
	"label": {}, "limit": {}, "depth": {}, "strings": {}, "stream": {},
}

// printConfig is the parsed subset of @{ ... } args that runtime.print
// consumes. Defaults are applied when a field is missing or invalid.
type printConfig struct {
	label   string
	limit   int
	depth   int
	strings int
	stream  string
}

// Print renders the incoming value to stdout or stderr with a bound on
// depth, item count per container, and string length. It is a clean
// tap: its own config keys are stripped from the value that continues
// downstream, so a debug insertion does not change the pipeline shape.
var Print = action.New("runtime.print", func(_ context.Context, in any) (any, error) {
	cfg := extractPrintConfig(in)
	data := stripPrintConfig(in)

	writer := printStream(cfg.stream)
	rendered := renderPrint(data, cfg, printIsTerminal(writer))
	writePrintOutput(writer, cfg.label, rendered)

	return data, nil
}).
	Description("Pretty-print the input with size limits; strips its own config keys before passing through").
	Tag("base", "debug", "print").
	Build()

func printStream(name string) io.Writer {
	if name == "stdout" {
		return os.Stdout
	}
	return os.Stderr
}

func printIsTerminal(w io.Writer) bool {
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

func writePrintOutput(w io.Writer, label, rendered string) {
	if label != "" {
		fmt.Fprintf(w, "[%s] %s\n", label, rendered)
		return
	}
	fmt.Fprintln(w, rendered)
}

func extractPrintConfig(in any) printConfig {
	cfg := printConfig{
		limit:   printDefaultLimit,
		depth:   printDefaultDepth,
		strings: printDefaultStrings,
		stream:  "stderr",
	}
	m, ok := in.(map[string]any)
	if !ok {
		return cfg
	}
	if v, ok := m["label"].(string); ok {
		cfg.label = v
	}
	if v, ok := readPositiveInt(m["limit"]); ok {
		cfg.limit = v
	}
	if v, ok := readPositiveInt(m["depth"]); ok {
		cfg.depth = v
	}
	if v, ok := readPositiveInt(m["strings"]); ok {
		cfg.strings = v
	}
	if v, ok := m["stream"].(string); ok && (v == "stdout" || v == "stderr") {
		cfg.stream = v
	}
	return cfg
}

func readPositiveInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, x > 0
	case int64:
		return int(x), x > 0
	case float64:
		return int(x), x > 0
	}
	return 0, false
}

// stripPrintConfig removes runtime.print's own config keys from a map
// input. Non-map inputs are returned unchanged. Config keys that also
// exist in the user's data are still stripped — the action's namespace
// is reserved, matching the convention used by runtime.pick,
// runtime.wrap, and every other argument-taking runtime action.
func stripPrintConfig(in any) any {
	m, ok := in.(map[string]any)
	if !ok {
		return in
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if _, isConfig := printConfigKeys[k]; isConfig {
			continue
		}
		out[k] = v
	}
	return out
}

// renderPrint renders v to a string, stopping cleanly at printMaxBytes.
func renderPrint(v any, cfg printConfig, color bool) string {
	buf := &printBuffer{limit: printMaxBytes}
	renderPrintValue(buf, v, cfg, 0, color, "")
	return buf.finish()
}

// printBuffer is a strings.Builder with a hard byte budget. Every write
// goes through it so a pathological input cannot flood the terminal
// even when depth/limit/strings are permissive.
type printBuffer struct {
	sb        strings.Builder
	limit     int
	truncated bool
}

func (b *printBuffer) write(s string) {
	if b.truncated {
		return
	}
	if b.sb.Len()+len(s) > b.limit {
		remaining := b.limit - b.sb.Len()
		if remaining > 0 {
			b.sb.WriteString(s[:remaining])
		}
		b.truncated = true
		return
	}
	b.sb.WriteString(s)
}

func (b *printBuffer) finish() string {
	out := b.sb.String()
	if b.truncated {
		out += printDim + "\n... (output truncated)" + printReset
	}
	return out
}

func renderPrintValue(buf *printBuffer, v any, cfg printConfig, depth int, color bool, indent string) {
	if depth > cfg.depth {
		buf.write(paintPrint("...", printDim, color))
		return
	}
	switch x := v.(type) {
	case nil:
		buf.write(paintPrint("null", printDim, color))
	case bool:
		buf.write(paintPrint(strconv.FormatBool(x), printMagenta, color))
	case string:
		buf.write(paintPrint(strconv.Quote(truncatePrintString(x, cfg.strings)), printGreen, color))
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		buf.write(paintPrint(fmt.Sprint(x), printYellow, color))
	case map[string]any:
		renderPrintMap(buf, x, cfg, depth, color, indent)
	case []any:
		renderPrintSlice(buf, x, cfg, depth, color, indent)
	default:
		buf.write(paintPrint(fmt.Sprintf("%v", x), printCyan, color))
	}
}

func renderPrintMap(buf *printBuffer, m map[string]any, cfg printConfig, depth int, color bool, indent string) {
	if len(m) == 0 {
		buf.write("{}")
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	displayed := keys
	truncated := 0
	if cfg.limit > 0 && len(keys) > cfg.limit {
		truncated = len(keys) - cfg.limit
		displayed = keys[:cfg.limit]
	}

	inner := indent + "  "
	buf.write("{\n")
	for i, k := range displayed {
		buf.write(inner)
		buf.write(paintPrint(strconv.Quote(k), printGreen, color))
		buf.write(": ")
		renderPrintValue(buf, m[k], cfg, depth+1, color, inner)
		if i < len(displayed)-1 {
			buf.write(",")
		}
		buf.write("\n")
	}
	if truncated > 0 {
		buf.write(inner)
		buf.write(paintPrint(fmt.Sprintf("... %d more", truncated), printDim, color))
		buf.write("\n")
	}
	buf.write(indent)
	buf.write("}")
}

func renderPrintSlice(buf *printBuffer, s []any, cfg printConfig, depth int, color bool, indent string) {
	if len(s) == 0 {
		buf.write("[]")
		return
	}

	displayed := s
	truncated := 0
	if cfg.limit > 0 && len(s) > cfg.limit {
		truncated = len(s) - cfg.limit
		displayed = s[:cfg.limit]
	}

	if printSliceIsInline(displayed) {
		buf.write("[")
		for i, item := range displayed {
			if i > 0 {
				buf.write(", ")
			}
			renderPrintValue(buf, item, cfg, depth+1, color, indent)
		}
		if truncated > 0 {
			buf.write(paintPrint(fmt.Sprintf(", ... %d more", truncated), printDim, color))
		}
		buf.write("]")
		return
	}

	inner := indent + "  "
	buf.write("[\n")
	for i, item := range displayed {
		buf.write(inner)
		renderPrintValue(buf, item, cfg, depth+1, color, inner)
		if i < len(displayed)-1 {
			buf.write(",")
		}
		buf.write("\n")
	}
	if truncated > 0 {
		buf.write(inner)
		buf.write(paintPrint(fmt.Sprintf("... %d more", truncated), printDim, color))
		buf.write("\n")
	}
	buf.write(indent)
	buf.write("]")
}

func printSliceIsInline(items []any) bool {
	for _, item := range items {
		switch item.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
}

func truncatePrintString(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

func paintPrint(s, code string, enabled bool) string {
	if !enabled {
		return s
	}
	return code + s + printReset
}
