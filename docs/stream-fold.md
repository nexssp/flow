# Streaming folds

`stream.fold` reduces a stream to one accumulator value in a single pass. It
reuses Kernel's `stream.Reduce` primitive and keeps only the current
accumulator, so it is suitable for counters, totals, and other compact
aggregates without materializing the input stream.

```nflow
records -> stream.fold @{
  initial: { count: 0, bytes: 0 },
  expression: "{ count: acc.count + 1, bytes: acc.bytes + item.size }"
} -> collect
```

The `expression` is compiled once when the operator is built and evaluated for
each successful input record. It has two variables:

- `acc`: the current accumulator value, initialized from `initial`;
- `item`: the current stream record.

The expression result becomes the accumulator for the next record. The fold
emits exactly one value when the stream completes normally, including the
initial value for an empty stream. An upstream error stops the fold and is
forwarded; an invalid or failing expression produces a stream error.

`stream.fold` is a general aggregation primitive. Application-specific
selection, saved-state or Git filtering, ignore rules, scan guardrails,
language-aware parsing, and filesystem or clipboard behavior remain in their
existing application and package boundaries.
