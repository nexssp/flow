# CLI process signals

The `nflow` CLI treats **Ctrl+C/SIGINT** and **SIGTERM** as graceful cancellation requests. The first registered signal cancels the invocation context. The CLI waits for active Flow actions and joined child work to return, then runs the Host's bounded cleanup (10 seconds by default, subject to cleanup callbacks honoring their context).

Signal delivery is unregistered as soon as the first termination signal is received, before cancellation starts draining work. A repeated SIGINT/SIGTERM therefore uses the platform's default behavior instead of being swallowed throughout a stalled shutdown. An action or child that does not cooperate with context cancellation can still delay the first graceful drain; use a repeated termination signal to request the default forceful behavior where the platform provides it.

The handler intentionally does not register **SIGHUP** or **SIGQUIT**: SIGHUP remains available for explicit reload handling, and SIGQUIT retains Go's diagnostic stack-dump behavior. **SIGKILL** and **SIGSTOP** cannot be caught by a process.

On Windows, Ctrl-C and Ctrl-Break are reported as `os.Interrupt`; close, logoff, and shutdown events can be reported as SIGTERM. Those events give the process an opportunity to clean up, but Windows does not guarantee that process termination is delayed while cleanup runs. Repeated-signal behavior follows the operating system's default rules.

This changes only operating-system cancellation of CLI invocations; it does not change Flow DSL signal or event semantics. APIs that accept a caller-provided context continue to use that context as the cancellation authority.
