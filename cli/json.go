package cli

import (
	"encoding/json"
	"io"
)

// writeJSON writes v as JSON to w with HTML escaping disabled.
//
// pretty controls indentation. Every CLI command that emits JSON on
// stdout passes true; `nflow catalog` passes whatever its --pretty flag
// resolved to, so a script that diffs catalogs can request the compact
// form.
//
// Go's json.Encoder escapes <, >, and & by default because it was
// designed to be safe when JSON is embedded inside HTML <script> tags.
// CLI output is not embedded in HTML; the escaping only makes the
// output harder to read and produces surprises like "<test>" appearing
// as "\u003ctest\u003e" in a diff.
func writeJSON(w io.Writer, v any, pretty bool) error {
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "  ")
	}
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
