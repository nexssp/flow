// Package capability resolves flow-node names into runnable action.AnyAction
// values at flow-execution time.
//
// Three resolution backends are supported, tried in this order:
//
//  1. Static — the caller-supplied registry (in-process actions).
//  2. Remote — HTTP/JSON or exec-based proxies declared in the .flow manifest
//     via :remote= and :exec= modifiers.
//  3. WASM   — WebAssembly modules loaded via wazero, declared via :wasm= in
//     the manifest.
//
// Static lookups always win: an in-process action shadows a manifest-declared
// remote with the same name. This lets callers override individual
// capabilities during development without editing the .flow manifest.
//
// Manifest syntax (all modifiers are colon-separated, in any order):
//
//	agent.orchestrator:remote="http://10.0.0.5:9001/solve":timeout=30s@Prompt hint
//	tools.ripgrep:exec="rg --json":timeout=10s@Search files
//	skills.lint:wasm="./skills/lint.wasm":timeout=15s@Lint Go code
//
// The annotation (everything after the first unquoted '@') is stripped by the
// parser and is never passed to the runtime.
package capability
