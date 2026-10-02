// Package external provides three actions for calling code that lives
// outside the Flow process: exec (shell), http.request (HTTP client),
// and wasm (Wazero WASI modules). This is the polyglot escape hatch —
// any repository can expose a Bundle that wraps its own logic through
// one of these.
//
// Typical use:
//
//	external.exec @{ cmd: "python3 enhancer.py", input: .payload }
//	http.request @{ url: "https://api.example/v1/items", method: "GET" }
//	external.wasm @{ path: "plugins/transform.wasm", input: .data }
package external

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "external"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		SelfTest:  selftest,
	}
}

func Library() action.Library {
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			Exec,
			HTTPRequest,
			WASM,
		},
	}
}

// ExecResult is the shape returned by Exec and WASM. It carries both
// the raw streams and, when stdout parses as JSON, the structured
// Output. Ok is a convenience flag for exit_code == 0.
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	OK       bool   `json:"ok"`
	Output   any    `json:"output,omitempty"`
}
