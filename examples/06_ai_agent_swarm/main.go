package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

type QueryInput struct {
	Prompt string `json:"prompt"`
}

type KnowledgeResult struct {
	Query   string `json:"query"`
	Context string `json:"context"`
}

type CodeSpec struct {
	Prompt string `json:"prompt"`
}

type CodeArtifact struct {
	SourceCode string `json:"source_code"`
	Version    int    `json:"version"`
}

type ReviewVerdict struct {
	SecAudit  string `json:"sec_audit"`
	PerfAudit string `json:"perf_audit"`
}

type SwarmDecision struct {
	Approved bool   `json:"approved"`
	Verdict  string `json:"verdict"`
}

func main() {
	ctx := context.Background()

	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("[nexssp/flow] Architecture 06: Multi-Agent Swarm with Parallel Peer Review")
	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("• Topology  : RAG Retrieval -> Synthesis -> Parallel Audits -> Consensus Gate")
	fmt.Println("• Type Model: Strongly-typed Go value structs (Zero heap allocs on stack)")
	fmt.Println(strings.Repeat("─", 78))

	retriever := action.New("retriever", func(_ context.Context, in QueryInput) (KnowledgeResult, error) {
		fmt.Printf("   🔍 [Agent 1/5] retriever  | Context lookup for: %q\n", in.Prompt)

		return KnowledgeResult{
			Query:   in.Prompt,
			Context: "Nexss Flow provides zero-allocation atomic proxies and in-memory AST graphs.",
		}, nil
	}).Build()

	coder := action.New("coder", func(_ context.Context, in CodeSpec) (CodeArtifact, error) {
		fmt.Printf("   💻 [Agent 2/5] coder      | Generating verified Go code artifact\n")

		return CodeArtifact{SourceCode: "package main; func Execute() {}", Version: 1}, nil
	}).Build()

	secAudit := action.New("sec_audit", func(_ context.Context, art CodeArtifact) (string, error) {
		time.Sleep(10 * time.Millisecond)
		fmt.Println("   🛡️  [Agent 3/5] sec_audit  | [PARALLEL A] Security scan PASS: 0 CVEs")

		return "SEC_PASS", nil
	}).Build()

	perfAudit := action.New("perf_audit", func(_ context.Context, art CodeArtifact) (string, error) {
		time.Sleep(15 * time.Millisecond)
		fmt.Println("   ⚡ [Agent 4/5] perf_audit | [PARALLEL B] Performance profile PASS: 0 heap allocs")

		return "PERF_PASS: 0 allocs/op", nil
	}).Build()

	judge := action.New("judge", func(_ context.Context, v ReviewVerdict) (SwarmDecision, error) {
		fmt.Printf("   ⚖️  [Agent 5/5] judge      | Consensus verified: [%s] & [%s]\n", v.SecAudit, v.PerfAudit)
		ok := strings.Contains(v.SecAudit, "PASS") && strings.Contains(v.PerfAudit, "PASS")

		return SwarmDecision{Approved: ok, Verdict: "Swarm consensus verified for deployment"}, nil
	}).Build()

	registry := flow.NewRegistry(retriever, coder, secAudit, perfAudit, judge)

	dsl := `
		retriever
		-> { prompt: "Context: " + context }
		-> coder
		-> ( sec_audit & perf_audit )
		-> { sec_audit: sec_audit, perf_audit: perf_audit }
		-> judge
	`

	fmt.Printf("⚡ Arrow Swarm DSL:\n%s\n\n", strings.TrimSpace(dsl))

	pipeline, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		panic(err)
	}

	execStart := time.Now()

	res, err := pipeline.Build().Do(ctx, QueryInput{Prompt: "Compile zero-allocation swarm"})
	if err != nil {
		panic(err)
	}

	decision := res.(SwarmDecision)

	fmt.Println(strings.Repeat("─", 78))
	fmt.Printf("• Swarm Status : %s (Approved=%v)\n", decision.Verdict, decision.Approved)
	fmt.Printf("• Total Latency: %v (Effective latency = max(10ms, 15ms) + runtime)\n", time.Since(execStart))
	fmt.Println(strings.Repeat("═", 78))
}
