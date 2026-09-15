// Package runner provides the shared dynamic-flow execution pipeline used by
// both `nexssflow` (standalone binary) and `nexssp flow` (subcommand).
//
// It is deliberately independent of any specific transport: callers supply a
// flow file path, an initial payload, and optionally a list of assertions;
// the runner handles DSL sanitization, capability resolution, execution, and
// assertion evaluation.
//
// The pipeline is:
//
//	read manifest
//	  -> merge @assert: directives with caller assertions
//	  -> sanitize DSL (strip comments, headers, inline @-annotations)
//	  -> build static registry (AI bundle + canonical aliases)
//	  -> parse :remote / :exec / :wasm bindings from the manifest
//	  -> materialize proxy actions, install into registry
//	  -> compile + execute the flow
//	  -> evaluate assertions against the JSON-encoded result
//
// Capability resolution is done by the child package
// github.com/nexssp/flow/runner/capability.
package runner
