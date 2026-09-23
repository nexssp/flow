// Package transport is the sole extension point between the domainless
// Flow compiler and concrete transport adapters.
//
// # The contract
//
// A transport library provides:
//
//  1. A Resolver — converts DSL modifiers into action.Binding values
//     at compile time.
//
//  2. A RuntimeProvider — opens a live connection for `nexssflow serve`.
//
//  3. A Library(...) function — returns the actions the transport adds
//     to the pipeline (for example nats.publish, http.get).
//
//  4. An init() function — calls RegisterResolver and
//     RegisterRuntimeProvider so the compiler sees the transport.
//
// # What belongs here
//
// Only the interfaces and the registry. No concrete transport.
//
// # What does NOT belong here
//
// Anything that imports net/http, nats.go, os/exec, or a transport
// package. This package must remain dependency-free apart from
// kernel/action and flow/dslparse.
//
// # Example transport
//
// See github.com/nexssp/transportnats/nexssflow for a complete example.
package transport
