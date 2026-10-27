# `data` extension

`data` adds a structured JSON Lines (`.jsonl`) source to Flow. During preprocessing, `@data` records the source path and checks that it is a regular file; it does not parse or retain the rows. At execution time, `data.rows` opens the file and decodes one object at a time. **The file is re-read on every invocation; `@data` does not cache rows.** Declared datasets inherit into child pipeline compilation, and the source reader is closed when its stream ends or the per-run resource scope shuts down.

```nflow
@data order_cases :from="order-cases.jsonl"
@datause order_cases
-> data.split:partition="train" :key="id" :train=80 :validation=10 :test=10 :seed=42
-> data.where:expression="is_refund == true"
-> data.select:fields="id,text,label"
-> collect
```

For top-level flow files, `@data` paths are relative to the `.nflow` file. Inline child pipeline source names are synthetic, so a child-local relative `@data` path currently resolves from the process working directory; prefer declaring the dataset in the parent and using `@datause` in the child. Blank lines are ignored; every non-blank record must be a JSON object. JSON syntax is checked as rows are read, so malformed input fails during execution with a file and line number. A JSONL record may be at most 16 MiB. `@datause` requires a prior declaration in the current or inherited pipeline scope; redeclaring an inherited dataset name is an error.

`data.where` accepts an expr-lang boolean expression over row fields and the `row`/`item` aliases. `data.select` keeps a comma-separated set of top-level fields; an absent requested field is included with a `nil` value. Use `fields="*"` to copy every field.

`data.split` uses a stable hash of the selected key field and seed. `key` defaults to `id`; keys must be present and non-null, and equal keys always go to the same partition. The percentage thresholds must be between 0 and 100 and add to 100. Membership is reproducible for the same key and seed regardless of input order. Counts are approximate, not exact: even large datasets vary around the requested proportions, and small datasets can differ substantially. The splitter retains only the current row and key encoding. Exact-count splitting is not included; it would require a separate, slower pass or buffering mode. Downstream operators such as `collect` may still retain output rows.

See [`nflows/master_example.nflow`](nflows/master_example.nflow) for the complete runnable example.
