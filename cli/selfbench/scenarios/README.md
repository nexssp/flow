# Adding a self benchmark

Add a self-contained `.nflow` file anywhere below this folder. Its relative path
is its name and filter key. No Go registration or manifest is needed. Rebuild
`nflow` to refresh embedded files, then use `nflow self bench --list` to see it.
For a custom suite without rebuilding, use `nflow self bench --path FILE_OR_DIR`.

Keep the default suite deterministic and local: no public services, network
access, external processes, credentials, sleeps, or persistent writes. Each file
should create its own representative input and produce a result without printing.
Use canonical qualified action names and only capabilities available in the CLI.

Include at least one `@assert:` with the expected result. The generic
`TestBuiltinScenarios` test discovers every file and checks those assertions
outside measurement. Run `go test ./cli/selfbench` after adding a scenario.
The benchmark command itself is not a correctness test and does not evaluate
these assertions in its timed loops.

A scenario is compiled once, executed once for `first_run_ms`, warmed up the
requested number of additional times, and then sampled for `--iterations` warm
runs. The compiled program and its state are reused, while each execution gets
a fresh payload map and execution identity. `compile_ms` and `first_run_ms` are
in-process measurements, not process cold-start timings. Warm samples include
the normal runner execution wrapper. Custom flows may have arbitrary side
effects, just like `nflow run`; only benchmark files you trust.
