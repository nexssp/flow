package flow

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const APIVersion = "nexss.ai/v1"

type NodeKind string

const (
	NodeDeterministic NodeKind = "deterministic"
	NodeLLM           NodeKind = "llm"
	NodeTool          NodeKind = "tool"
	NodeSubgraph      NodeKind = "subgraph"
	NodeHuman         NodeKind = "human"
	NodeApproval      NodeKind = "approval"
)

type BranchMode string

const (
	BranchFirstMatch BranchMode = "first_match"
	BranchAllMatches BranchMode = "all_matches"
)

type EffectClass string

const (
	EffectReadOnly   EffectClass = "read_only"
	EffectSideEffect EffectClass = "side_effect"
	EffectHighRisk   EffectClass = "high_risk"
)

type Metadata struct {
	Name        string `json:"name" yaml:"name"`
	Version     string `json:"version" yaml:"version"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts int    `json:"max_attempts,omitempty" yaml:"max_attempts,omitempty"`
	Backoff     string `json:"backoff,omitempty" yaml:"backoff,omitempty"`
}

type BudgetPolicy struct {
	MaxCostUSD          *float64 `json:"max_cost_usd,omitempty" yaml:"max_cost_usd,omitempty"`
	MaxCostUSDPerRun    *float64 `json:"max_cost_usd_per_run,omitempty" yaml:"max_cost_usd_per_run,omitempty"`
	MaxCostUSDPerDay    *float64 `json:"max_cost_usd_per_day,omitempty" yaml:"max_cost_usd_per_day,omitempty"`
	MaxCostUSDPerMonth  *float64 `json:"max_cost_usd_per_month,omitempty" yaml:"max_cost_usd_per_month,omitempty"`
	MaxCostUSDPerTenant *float64 `json:"max_cost_usd_per_tenant,omitempty" yaml:"max_cost_usd_per_tenant,omitempty"`
}

type GraphPolicy struct {
	Budget              BudgetPolicy   `json:"budget,omitempty" yaml:"budget,omitempty"`
	MaxParallelNodes    int            `json:"max_parallel_nodes,omitempty" yaml:"max_parallel_nodes,omitempty"`
	MaxContextBytes     int64          `json:"max_context_bytes,omitempty" yaml:"max_context_bytes,omitempty"`
	ApprovalRequiredFor []EffectClass  `json:"approval_required_for,omitempty" yaml:"approval_required_for,omitempty"`
	FanInRecovery       RecoveryPolicy `json:"fan_in_recovery,omitempty" yaml:"fan_in_recovery,omitempty"`
}

type NodeSpec struct {
	ID                  string            `json:"id" yaml:"id"`
	Kind                NodeKind          `json:"kind" yaml:"kind"`
	Capability          string            `json:"capability" yaml:"capability"`
	Params              map[string]any    `json:"params,omitempty" yaml:"params,omitempty"`
	InputBindings       map[string]string `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	InputSchema         string            `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	OutputSchema        string            `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Prompt              string            `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Retry               RetryPolicy       `json:"retry,omitempty" yaml:"retry,omitempty"`
	TimeoutMS           int64             `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	MaxAttempts         int               `json:"max_attempts,omitempty" yaml:"max_attempts,omitempty"`
	EstimatedCostMicros int64             `json:"estimated_cost_micros,omitempty" yaml:"estimated_cost_micros,omitempty"`
	Effect              EffectClass       `json:"effect,omitempty" yaml:"effect,omitempty"`
	Approval            bool              `json:"approval_required,omitempty" yaml:"approval_required,omitempty"`
	BranchMode          BranchMode        `json:"branch_mode,omitempty" yaml:"branch_mode,omitempty"`
}

type EdgeSpec struct {
	From      string       `json:"from" yaml:"from"`
	To        string       `json:"to" yaml:"to"`
	When      string       `json:"when,omitempty" yaml:"when,omitempty"`
	Otherwise bool         `json:"otherwise,omitempty" yaml:"otherwise,omitempty"`
	Priority  int          `json:"priority,omitempty" yaml:"priority,omitempty"`
	Budget    BudgetPolicy `json:"budget,omitempty" yaml:"budget,omitempty"`
}

type GraphDefinition struct {
	APIVersion string      `json:"apiVersion" yaml:"apiVersion"`
	Kind       string      `json:"kind" yaml:"kind"`
	Metadata   Metadata    `json:"metadata" yaml:"metadata"`
	Policy     GraphPolicy `json:"policy,omitempty" yaml:"policy,omitempty"`
	Nodes      []NodeSpec  `json:"nodes" yaml:"nodes"`
	Edges      []EdgeSpec  `json:"edges" yaml:"edges"`
}

type CompiledBudget struct {
	MaxCostMicrosPerRun    int64
	MaxCostMicrosPerDay    int64
	MaxCostMicrosPerMonth  int64
	MaxCostMicrosPerTenant int64
}

type CompiledEdge struct {
	From      string
	To        string
	When      string
	Otherwise bool
	Priority  int
	Budget    CompiledBudget
}

type CompiledGraph struct {
	Definition GraphDefinition
	Layers     [][]string
	NodeByID   map[string]NodeSpec
	Edges      []CompiledEdge
	Outgoing   map[string][]CompiledEdge
	EdgeKeys   []string
	Budget     CompiledBudget
}

var conditionPattern = regexp.MustCompile(`^state\.[A-Za-z_][A-Za-z0-9_.]*\s+(?:(?:==|!=|>=|<=|>|<)\s+(?:"[^"]*"|'[^']*'|true|false|-?[0-9]+(?:\.[0-9]+)?)|exists)$`)

func (g *CompiledGraph) SelectOutgoing(source string, matches func(condition string) (bool, error)) ([]CompiledEdge, error) {
	edges := g.Outgoing[source]
	if len(edges) == 0 {
		return nil, nil
	}

	conditional := edges[0].When != "" || edges[0].Otherwise
	if conditional && matches == nil {
		return nil, fmt.Errorf("graph: conditional routing requires an evaluator function")
	}
	if !conditional {
		return append([]CompiledEdge(nil), edges...), nil
	}

	mode := BranchFirstMatch
	if node, ok := g.NodeByID[source]; ok && node.BranchMode != "" {
		mode = node.BranchMode
	}
	if mode != BranchFirstMatch && mode != BranchAllMatches {
		return nil, fmt.Errorf("graph: unsupported branch mode %q for node %q", mode, source)
	}

	var fallback *CompiledEdge
	var matchesFound []CompiledEdge

	for i := range edges {
		e := edges[i]
		if e.Otherwise {
			fallback = &e
			continue
		}
		ok, err := matches(e.When)
		if err != nil {
			return nil, fmt.Errorf("graph: evaluate edge %s -> %s: %w", e.From, e.To, err)
		}
		if ok {
			if mode == BranchFirstMatch {
				return []CompiledEdge{e}, nil
			}
			matchesFound = append(matchesFound, e)
		}
	}

	if len(matchesFound) > 0 {
		return matchesFound, nil
	}
	if fallback != nil {
		return []CompiledEdge{*fallback}, nil
	}
	return nil, nil
}

func Compile(def GraphDefinition) (*CompiledGraph, error) {
	if err := validate(def); err != nil {
		return nil, err
	}

	nodes := make(map[string]NodeSpec, len(def.Nodes))
	indegree := make(map[string]int, len(def.Nodes))

	for i := range def.Nodes {
		n := def.Nodes[i]
		nodes[n.ID] = n
		indegree[n.ID] = 0
	}

	adj := make(map[string][]string, len(nodes))
	edgeKeys := make([]string, 0, len(def.Edges))
	compiledEdges := make([]CompiledEdge, 0, len(def.Edges))
	outgoing := make(map[string][]CompiledEdge, len(nodes))
	seenEdges := make(map[string]struct{}, len(def.Edges))

	for _, e := range def.Edges {
		key := e.From + "\x00" + e.To
		if _, ok := seenEdges[key]; ok {
			return nil, fmt.Errorf("graph: duplicate edge %s -> %s", e.From, e.To)
		}
		seenEdges[key] = struct{}{}

		ce := CompiledEdge{
			From:      e.From,
			To:        e.To,
			When:      strings.TrimSpace(e.When),
			Otherwise: e.Otherwise,
			Priority:  e.Priority,
			Budget:    compileBudget(e.Budget),
		}
		compiledEdges = append(compiledEdges, ce)
		outgoing[e.From] = append(outgoing[e.From], ce)
		adj[e.From] = append(adj[e.From], e.To)
		indegree[e.To]++
		edgeKeys = append(edgeKeys, e.From+" -> "+e.To)
	}

	for id := range adj {
		sort.Strings(adj[id])
		sort.Slice(outgoing[id], func(i, j int) bool {
			if outgoing[id][i].Priority != outgoing[id][j].Priority {
				return outgoing[id][i].Priority < outgoing[id][j].Priority
			}
			if outgoing[id][i].Otherwise != outgoing[id][j].Otherwise {
				return !outgoing[id][i].Otherwise
			}
			return outgoing[id][i].To < outgoing[id][j].To
		})
	}
	sort.Strings(edgeKeys)

	layers := make([][]string, 0)
	remaining := len(nodes)

	for remaining > 0 {
		layer := make([]string, 0)
		for id, degree := range indegree {
			if degree == 0 {
				layer = append(layer, id)
			}
		}
		if len(layer) == 0 {
			return nil, fmt.Errorf("graph: contains a cyclic dependency")
		}
		sort.Strings(layer)
		layers = append(layers, layer)

		for _, id := range layer {
			delete(indegree, id)
			remaining--
			for _, child := range adj[id] {
				indegree[child]--
			}
		}
	}

	return &CompiledGraph{
		Definition: def,
		Layers:     layers,
		NodeByID:   nodes,
		Edges:      compiledEdges,
		Outgoing:   outgoing,
		EdgeKeys:   edgeKeys,
		Budget:     compileBudget(def.Policy.Budget),
	}, nil
}

func validate(def GraphDefinition) error {
	if def.APIVersion != APIVersion {
		return fmt.Errorf("graph: unsupported apiVersion %q (expected %q)", def.APIVersion, APIVersion)
	}
	if def.Kind != "Graph" {
		return fmt.Errorf("graph: kind must be \"Graph\"")
	}
	if def.Metadata.Name == "" || def.Metadata.Version == "" {
		return fmt.Errorf("graph: metadata.name and metadata.version are required")
	}
	if len(def.Nodes) == 0 {
		return fmt.Errorf("graph: at least one node is required")
	}

	seen := map[string]bool{}

	for i := range def.Nodes {
		n := &def.Nodes[i]

		if n.ID == "" || seen[n.ID] {
			return fmt.Errorf("graph: node IDs must be non-empty and unique: %q", n.ID)
		}
		seen[n.ID] = true

		if n.Kind == "" {
			n.Kind = NodeTool
		}
		switch n.Kind {
		case NodeDeterministic, NodeLLM, NodeTool, NodeSubgraph, NodeHuman, NodeApproval:
		default:
			return fmt.Errorf("graph: node %q has unsupported kind %q", n.ID, n.Kind)
		}

		if n.Capability == "" {
			return fmt.Errorf("graph: node %q requires a capability reference", n.ID)
		}
		if n.BranchMode != "" && n.BranchMode != BranchFirstMatch && n.BranchMode != BranchAllMatches {
			return fmt.Errorf("graph: node %q has unsupported branch mode %q", n.ID, n.BranchMode)
		}
		if n.TimeoutMS < 0 || n.MaxAttempts < 0 || n.Retry.MaxAttempts < 0 || n.EstimatedCostMicros < 0 {
			return fmt.Errorf("graph: node %q has negative execution limit", n.ID)
		}
	}

	otherwiseBySource := map[string]bool{}
	for _, e := range def.Edges {
		if e.From == "" || e.To == "" || !seen[e.From] || !seen[e.To] {
			return fmt.Errorf("graph: edge references unknown node: %q -> %q", e.From, e.To)
		}
		if e.From == e.To {
			return fmt.Errorf("graph: self-edge is not allowed: %q", e.From)
		}
		if e.Priority < 0 {
			return fmt.Errorf("graph: edge %q -> %q has negative priority", e.From, e.To)
		}
		if err := validateBudget(e.Budget, fmt.Sprintf("edge %q -> %q budget", e.From, e.To)); err != nil {
			return err
		}
		if e.Otherwise {
			if strings.TrimSpace(e.When) != "" {
				return fmt.Errorf("graph: edge %q -> %q cannot define both when and otherwise", e.From, e.To)
			}
			if otherwiseBySource[e.From] {
				return fmt.Errorf("graph: source node %q has multiple otherwise edges", e.From)
			}
			otherwiseBySource[e.From] = true
		} else if strings.TrimSpace(e.When) != "" {
			if !conditionPattern.MatchString(strings.TrimSpace(e.When)) {
				return fmt.Errorf("graph: edge %q -> %q has unsupported condition syntax %q", e.From, e.To, e.When)
			}
		}
	}

	bySource := map[string][]EdgeSpec{}
	for _, e := range def.Edges {
		bySource[e.From] = append(bySource[e.From], e)
	}
	for source, edges := range bySource {
		conditional := false
		unconditional := false
		for _, e := range edges {
			conditional = conditional || e.When != "" || e.Otherwise
			unconditional = unconditional || (e.When == "" && !e.Otherwise)
		}
		if conditional && unconditional {
			return fmt.Errorf("graph: source node %q mixes conditional and unconditional edges", source)
		}
	}

	if def.Policy.MaxParallelNodes < 0 || def.Policy.MaxContextBytes < 0 {
		return fmt.Errorf("graph: policy limits cannot be negative")
	}
	if err := validateEffects(def.Policy.ApprovalRequiredFor); err != nil {
		return err
	}
	if err := validateBudget(def.Policy.Budget, "graph budget"); err != nil {
		return err
	}

	strat := def.Policy.FanInRecovery.Strategy
	if strat != "" && strat != RecoveryFailFast && strat != RecoveryRetryFailed && strat != RecoveryContinuePartial {
		return fmt.Errorf("graph: unsupported fan-in recovery strategy %q", strat)
	}
	if def.Policy.FanInRecovery.MaxAttempts < 0 || def.Policy.FanInRecovery.BackoffMS < 0 || def.Policy.FanInRecovery.MaxBackoffMS < 0 {
		return fmt.Errorf("graph: fan-in recovery limits cannot be negative")
	}

	return nil
}

func validateEffects(effects []EffectClass) error {
	for _, e := range effects {
		if e != EffectReadOnly && e != EffectSideEffect && e != EffectHighRisk {
			return fmt.Errorf("graph: unsupported approval effect %q", e)
		}
	}
	return nil
}

func validateBudget(b BudgetPolicy, prefix string) error {
	for name, value := range map[string]*float64{
		"max_cost_usd":            b.MaxCostUSD,
		"max_cost_usd_per_run":    b.MaxCostUSDPerRun,
		"max_cost_usd_per_day":    b.MaxCostUSDPerDay,
		"max_cost_usd_per_month":  b.MaxCostUSDPerMonth,
		"max_cost_usd_per_tenant": b.MaxCostUSDPerTenant,
	} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return fmt.Errorf("graph: %s.%s must be finite and non-negative", prefix, name)
		}
	}
	return nil
}

func compileBudget(b BudgetPolicy) CompiledBudget {
	perRun := b.MaxCostUSDPerRun
	if perRun == nil {
		perRun = b.MaxCostUSD
	}
	return CompiledBudget{
		MaxCostMicrosPerRun:    toMicros(perRun),
		MaxCostMicrosPerDay:    toMicros(b.MaxCostUSDPerDay),
		MaxCostMicrosPerMonth:  toMicros(b.MaxCostUSDPerMonth),
		MaxCostMicrosPerTenant: toMicros(b.MaxCostUSDPerTenant),
	}
}

func toMicros(value *float64) int64 {
	if value == nil {
		return 0
	}
	return int64(math.Round(*value * 1_000_000))
}

func LoadYAML(data []byte) (*CompiledGraph, error) {
	var def GraphDefinition
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&def); err != nil {
		return nil, fmt.Errorf("graph: decode YAML: %w", err)
	}
	return Compile(def)
}

func LoadYAMLFile(path string) (*CompiledGraph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("graph: read YAML file %q: %w", path, err)
	}
	return LoadYAML(data)
}
