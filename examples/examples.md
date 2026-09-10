## 1. Go functions

```go
// action: pure Go function
sum := action.New("math.sum", func(_ context.Context, nums []int) (int, error) {
    total := 0
    for _, n := range nums {
        total += n
    }
    return total, nil
}).Build()

// DSL: pipeline with inline projection
dsl := `math.sum -> { result: . }`
```

---

## 2. SQL transactions

```go
// action: runs a SQL transaction
createOrder := action.New("db.create_order", func(ctx context.Context, req OrderReq) (Order, error) {
    tx, _ := db.BeginTx(ctx, nil)
    defer tx.Rollback()
    // insert, update...
    tx.Commit()
    return order, nil
}).Build()

// DSL: chain SQL with validation and notification
dsl := `
    db.validate_cart
    -> db.create_order
    -> { order_id: .id, status: "placed" }
    -> notify.slack
`
```

---

## 3. HTTP APIs

```go
// action: HTTP client call
fetchUser := action.New("http.get_user", func(ctx context.Context, id string) (User, error) {
    resp, _ := http.Get("https://api.example.com/users/" + id)
    // decode...
    return user, nil
}).Build()

// DSL: fetch, reshape, post
dsl := `
    http.get_user
    -> { name: .full_name, email: .primary_email }
    -> http.update_profile
`
```

---

## 4. File pipelines

```go
// actions: read, transform, write
read := action.New("file.read", func(_ context.Context, path string) ([]byte, error) {
    return os.ReadFile(path)
}).Build()

transform := action.New("file.transform", func(_ context.Context, data []byte) ([]byte, error) {
    return bytes.ToUpper(data), nil
}).Build()

write := action.New("file.write", func(_ context.Context, req struct{ Path string; Data []byte }) error {
    return os.WriteFile(req.Path, req.Data, 0644)
}).Build()

// DSL: read → transform → write
dsl := `
    file.read
    -> file.transform
    -> { path: "/tmp/output.txt", data: . }
    -> file.write
`
```

---

## 5. Edge nodes

```go
// action: edge node with region
edgeEU := action.New("edge.eu", func(_ context.Context, payload any) (EdgeRes, error) {
    // forward to EU region, measure latency, ring telemetry
    return EdgeRes{Region: "eu", LatencyMs: 12}, nil
}).Build()

// DSL: scatter to multiple edges and gather
dsl := `
    ( edge.eu & edge.us & edge.asia )
    -> { eu: .edge_eu, us: .edge_us, asia: .edge_asia }
`
```

---

## 6. AI agents

```go
// action: LLM call
llm := action.New("ai.llm", func(_ context.Context, prompt string) (string, error) {
    return provider.Complete(ctx, llm.Request{Messages: []llm.Message{{Role: "user", Content: prompt}}})
}).Build()

// DSL: retrieve, prompt, summarize, audit in parallel
dsl := `
    vector.search
    -> { context: .results, question: .user_query }
    -> ai.llm
    -> ( ai.sec_audit & ai.perf_audit )
    -> judge
`
```

---

## 7. New language toolchains

```go
// action: compile Zig/Go/Rust in sandbox
compileZig := action.New("lang.zig.build", func(ctx context.Context, src string) (Artifact, error) {
    // writes src, runs `zig build-exe`, returns binary
    return Artifact{Binary: bin, Size: len(bin)}, nil
}).Build()

run := action.New("lang.run", func(_ context.Context, bin []byte) (string, error) {
    // execute in sandbox with timeout
    return output, nil
}).Build()

// DSL: compile → execute → parse output
dsl := `
    lang.zig.build
    -> lang.run
    -> { output: ., passes: contains(., "PASS") }
`
```

---

## 8. Any typed action

```go
// action with custom struct types
type ComplexReq struct {
    A int
    B string
    C []float64
}
type ComplexRes struct {
    Sum int
    Concat string
    Avg float64
}

process := action.New("custom.process", func(_ context.Context, req ComplexReq) (ComplexRes, error) {
    return ComplexRes{
        Sum:    req.A + len(req.C),
        Concat: req.B + "_done",
        Avg:    avg(req.C),
    }, nil
}).Build()

// DSL: fully typed, projection extracts fields
dsl := `
    custom.process
    -> { total: .Sum, label: .Concat }
    -> another.action
`
```
