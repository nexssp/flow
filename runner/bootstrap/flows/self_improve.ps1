go run ./cmd/jumalu flow ./flows/self_improve.flow '{"goal":"Optimize ring buffer"}' \
  --assert="success == true" \
  --assert="p95_ms < baseline_p95_ms"
