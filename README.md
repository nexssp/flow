# Nexss Flow (`nexssp/flow`)

[![Go Version](https://img.shields.io/badge/go-1.25-blue.svg)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![CI](https://github.com/nexssp/flow/actions/workflows/ci.yml/badge.svg)](https://github.com/nexssp/flow/actions/workflows/ci.yml)

**`nexssp/flow`** is a universal, zero-infrastructure, Go-native **Action Orchestration Engine & Arrow DSL**.

It enables developers and AI agents to weave isolated, typed Go actions (`github.com/nexssp/kernel/action`) into high-throughput DAGs, scatter-gather parallel groups, resilient fallback chains, and dynamic data-shaping pipelines **with zero Go glue code**.

---

## ⚡ Key Invariants

* **Universal Action Engine:** Orchestrates standard Go functions, microservices, DB queries, and AI Agents indiscriminately.
* **Inline Data Projections (`{ ... }`):** Reshapes data between nodes on the fly using `expr-lang/expr`. No manual DTO adapters needed.
* **Compact Arrow DSL:** Express complex execution topographies using concise inline text strings (`->`, `|`, `&`, `||`, `?`).
* **Zero-Allocation Hot Paths:** Memory-pooled state management and pre-compiled expression bytecode execution.
* **Durable Branch Journaling:** SQLite/SQL-backed branch recording for deterministic execution replay.
* **Cost & Budget Governance:** Exact microdollar ledger tracking with hard-stop budget boundaries.

---

## 🏗️ Architecture

```
                  ┌──────────────────────────────┐
                  │      Input Payload           │
                  └──────────────┬───────────────┘
                                 │
                                 ▼
                  ┌──────────────────────────────┐
                  │   github.get_issue           │
                  └──────────────┬───────────────┘
                                 │
                                 ▼
                  ┌──────────────────────────────┐
                  │ Inline Projection ({ ... })  │
                  │ Reshapes output for AI Agent │
                  └──────────────┬───────────────┘
                                 │
                                 ▼
                  ┌──────────────────────────────┐
                  │   ai.summarizer              │
                  └──────────────┬───────────────┘
                                 │
                                 ▼
                  ┌──────────────────────────────┐
                  │ Inline Projection ({ ... })  │
                  │ Reshapes output for Slack    │
                  └──────────────┬───────────────┘
                                 │
                                 ▼
                  ┌──────────────────────────────┐
                  │   slack.send                 │
                  └──────────────────────────────┘
```

---

## 📦 Installation

```bash
go get github.com/nexssp/flow@latest
```

---

## 🚀 Quickstart: Data-Shaping Pipeline

```go
package main

import (
	"context"
	"fmt"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

func main() {
	ctx := context.Background()

	// 1. Define isolated Actions
	getIssue := action.New("github.get_issue", func(_ context.Context, id int) (map[string]any, error) {
		return map[string]any{
			"issue": map[string]any{"id": id, "title": "Nil pointer in scheduler", "body": "Line 42 panics"},
		}, nil
	}).Build()

	summarize := action.New("ai.summarizer", func(_ context.Context, req map[string]any) (map[string]any, error) {
		return map[string]any{"summary": "Critical panic in scheduler", "urgency": "HIGH"}, nil
	}).Build()

	sendSlack := action.New("slack.send", func(_ context.Context, req map[string]any) (string, error) {
		return fmt.Sprintf("Posted to %s: %s", req["channel"], req["text"]), nil
	}).Build()

	// 2. Register Actions
	registry := flow.NewRegistry(getIssue, summarize, sendSlack)

	// 3. Express Workflow in Arrow DSL with Inline Shaping
	dsl := `
		github.get_issue
		-> { prompt: "Summarize: " + issue.title + " - " + issue.body }
		-> ai.summarizer
		-> { channel: "#alerts", text: "🚨 [" + urgency + "] " + summary }
		-> slack.send
	`

	// 4. Compile & Execute
	builder, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		panic(err)
	}

	result, err := builder.Build().Do(ctx, 999)
	if err != nil {
		panic(err)
	}

	fmt.Println(result)
	// Output: Posted to #alerts: 🚨 [HIGH] Critical panic in scheduler
}
```

---

## 📖 Arrow DSL Syntax Reference

| Operator | Syntax | Description |
|---|---|---|
| **Sequential Pipe** | `A -> B` or `A \| B` | Passes output of `A` as input to `B`. |
| **Inline Projection** | `{ x: .y, z: "val" }` | Reshapes incoming data on the fly using `expr`. |
| **Parallel Scatter-Gather** | `(A & B & C)` | Executes `A`, `B`, and `C` concurrently; aggregates output. |
| **Fallback Chain** | `A \|\| B` | Tries `A`. If `A` returns an error, executes `B` (`FirstSuccess`). |
| **Conditional Branch** | `gate ? target` | Evaluates boolean input from `gate`. If true, runs `target`. |

---

## 🛠️ Capability Discovery for AI Agents

Expose registered capabilities so AI agents can dynamically inspect actions and synthesize Flow DSLs:

```go
// Extract machine-readable capability specifications
capabilities := flow.ExtractCapabilities(registry)

// Expose /flow/catalog over HTTP/A2A
catalogAction := flow.BuildCatalogAction(registry)
```

---

## Contributing

Read [`CONTRIBUTING.md`](./CONTRIBUTING.md) before opening a pull request. Changes must be typed, covered by tests, and include benchmarks for performance-critical paths.

## Security

Report suspected vulnerabilities privately according to [`SECURITY.md`](./SECURITY.md).

## License

Apache License 2.0. See [`LICENSE`](./LICENSE).
Copyright © 2018–2026 Marcin Polak and Contributors.
