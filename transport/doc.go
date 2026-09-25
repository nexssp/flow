// Package transport is the small integration boundary between Flow and
// concrete transport libraries.
//
// A transport library contributes an action.Library through Register and may
// expose three kinds of behavior:
//
//   - runtime actions and streams, returned by its factory;
//   - DSL binding resolvers, represented by actions routed with OnDSL;
//   - listener entry points, represented by actions routed with OnTrigger.
//
// The compiler resolves DSL bindings through ResolveModifier. A runner can
// locate a listener through FindTrigger. Transport-specific connections,
// subscriptions, shutdown, and retries remain inside the concrete transport
// action; this package does not own goroutines or network clients.
//
// The cold path is intentionally dynamic: @require options are decoded from
// string values and action bindings are type-erased. The execution path after
// resolution is the typed Kernel action or stream selected by the transport.
//
// The contract is deliberately limited to:
//
//   - Register, Load, and KnownPrefixes for libraries;
//   - Decode for typed @require configuration;
//   - OnDSL and OnTrigger for binding discovery;
//   - ResolveModifier, FindTrigger, RequireTrigger, and AsLibrary for assembly.
//
// No concrete protocol belongs in this package. HTTP, NATS, CLI, cron, and
// other transports are separate modules that depend on this boundary.
package transport
