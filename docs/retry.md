# Retry configuration

The `retry` extension applies Kernel retry middleware to atoms carrying `:retry` or `:retry=N`. An explicit number sets the maximum retry count for that atom; bare `:retry` uses the configured `default_max` (default `3`). The default policy retries only errors Kernel classifies as transient.

## Options

When a host loads the retry bundle through `@require`, it accepts these options:

| Option | Default | Meaning |
|---|---|---|
| `default_max` | `3` | Retry count used by bare `:retry` |
| `backoff` | `exponential_jitter` | `constant`, `exponential`, or `exponential_jitter` |
| `min_backoff` | `10ms` | Constant delay or base delay for exponential strategies |
| `max_backoff` | `2s` | Cap for exponential strategies |
| `retry_all` | `false` | Retry every non-nil error instead of transient errors only |

For example:

```nflow
@require retry {
  default_max: "4"
  backoff: "exponential"
  min_backoff: "100ms"
  max_backoff: "3s"
  retry_all: "false"
}

service.call:retry
```

Durations use Go duration syntax. Unknown options, invalid values, non-positive retry counts or delays, and a maximum below the minimum are rejected.

## Native bundle hosts

Hosts that mount Flow's standard native bundle set can set the same policy without duplicating the `retry` bundle:

```go
bundles := native.BundlesWithRetryConfig(retry.Config{
    DefaultMax:  4,
    Backoff:     retry.BackoffExponential,
    MinBackoff:  100 * time.Millisecond,
    MaxBackoff:  3 * time.Second,
    RetryAll:    false,
})
```

`retry.ConfigFromOptions` is also available to strictly decode an options map, and `retry.BundleWithConfig` builds a retry bundle for custom bundle sets. The standard native defaults remain unchanged.

## Retry-all caution

`retry_all: true` uses Kernel's `Builder.RetryAll` and retries permanent as well as transient errors. A repeated operation can have side effects, so enable this only for idempotent actions or otherwise safe operations. It remains off by default.
