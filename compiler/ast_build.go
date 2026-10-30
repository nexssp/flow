package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/builtin"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfile"
)

func (*Atom) Analyze(CapabilityResolver) error { return nil }

func (a *Atom) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	if a == nil || strings.TrimSpace(a.Name) == "" {
		return nil, xerr.Validation("atom name is empty")
	}
	if bCtx.Resolver == nil {
		return nil, xerr.Internal("build: capability resolver is nil")
	}
	resolveConfigRefsInAtom(ctx, a)
	resolveSymbolsInAtom(ctx, a)

	act, ok := bCtx.Resolver.Action(a.Name)
	if !ok {
		return nil, xerr.NotFound("capability " + a.Name + " not registered")
	}
	if a.ConfigInject {
		return nil, xerr.Validation(
			"@config injection marker is supported only for stream sources and operators; use explicit @config.KEY values for unary actions")
	}

	act, err := runAtomAdvisers(a, act, bCtx, bCtx.Advisers)
	if err != nil {
		return nil, err
	}

	strict := (strictFromCtx(ctx) || hasModifier(a.Modifiers, "strict")) &&
		!hasModifier(a.Modifiers, "lenient")

	if strict {
		if typed, ok := act.(action.TypedPayload); ok && typed.ReqPayload() != nil {
			act = act.CloneWithHooks(strictHook())
		}
	}

	return finalizeAtomAction(a, bCtx, act)
}

func (p *PipeExpr) Analyze(r CapabilityResolver) error {
	nodes := flattenPipe(p)
	if idx, _ := findBoundary(nodes); idx >= 0 {
		return analyzeStreamSegment(r, nodes[:idx])
	}
	for _, node := range nodes {
		if err := node.Analyze(r); err != nil {
			return err
		}
	}
	for i := 1; i < len(nodes); i++ {
		left, leftOK := contractForExpr(r, nodes[i-1])
		right, rightOK := contractForExpr(r, nodes[i])
		if !leftOK || !rightOK {
			continue
		}
		if err := requireCompatible(left.Output, right.Input, left.Name, right.Name); err != nil {
			return err
		}
	}
	return nil
}

func (p *PipeExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	nodes := flattenPipe(p)

	idx, spec := findBoundary(nodes)
	if idx < 0 {
		if len(nodes) > 0 {
			if head, ok := nodes[0].(*Atom); ok {
				if _, isStream := bCtx.Resolver.Stream(head.Name); isStream {
					if collect, found := BoundaryByName("collect"); found {
						return buildSegmented(ctx, bCtx, nodes, len(nodes), collect)
					}
				}
			}
		}
		left, err := p.L.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
		right, err := p.R.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
		return inspectBuiltPair(bCtx, ComposedPipe(left, right), IntrospectionKindSequence,
			action.IntrospectionChild{Node: left, Relation: IntrospectionKindSequence, Role: "left"},
			action.IntrospectionChild{Node: right, Relation: IntrospectionKindSequence, Role: "right"}), nil
	}

	return buildSegmented(ctx, bCtx, nodes, idx, spec)
}

func flattenPipe(e Expr) []Expr {
	if p, ok := e.(*PipeExpr); ok {
		return append(flattenPipe(p.L), p.R)
	}
	return []Expr{e}
}

func rebuildPipe(nodes []Expr) Expr {
	if len(nodes) == 0 {
		return nil
	}
	e := nodes[0]
	for _, n := range nodes[1:] {
		e = &PipeExpr{L: e, R: n}
	}
	return e
}

func findBoundary(nodes []Expr) (int, BoundarySpec) {
	for i, n := range nodes {
		atom, ok := n.(*Atom)
		if !ok {
			continue
		}
		if spec, ok := BoundaryByName(atom.Name); ok {
			return i, spec
		}
	}
	return -1, BoundarySpec{}
}

func buildSegmented(
	ctx context.Context,
	bCtx *BuildContext,
	nodes []Expr,
	idx int,
	spec BoundarySpec,
) (action.AnyAction, error) {
	if idx == 0 {
		return nil, xerr.Validation(
			"boundary " + spec.Name + " cannot appear at the start of a pipeline; " +
				"it needs a stream source on its left")
	}

	leftNodes := nodes[:idx]
	var rightNodes []Expr
	if idx < len(nodes) {
		rightNodes = nodes[idx+1:]
	}

	for _, n := range rightNodes {
		if atom, ok := n.(*Atom); ok {
			if _, isBoundary := BoundaryByName(atom.Name); isBoundary {
				return nil, xerr.Validation(
					"only one stream boundary per pipeline is supported; " +
						atom.Name + " follows " + spec.Name)
			}
		}
	}

	stream, err := buildStreamSource(ctx, bCtx, leftNodes)
	if err != nil {
		return nil, err
	}

	consumer := spec.Consume(stream).Build()

	if len(rightNodes) == 0 {
		program := action.Dynamic(consumer).Build()
		return inspectBuiltChild(bCtx, program, "stream_consumer", action.IntrospectionChild{
			Node: stream, Relation: "consumes", Role: "stream",
		}), nil
	}

	rightAct, err := rebuildPipe(rightNodes).Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}

	consumerAction := inspectBuiltChild(bCtx, consumer, "stream_consumer", action.IntrospectionChild{
		Node: stream, Relation: "consumes", Role: "stream",
	})
	return inspectBuiltPair(bCtx, ComposedPipe(consumerAction, rightAct), "sequence",
		action.IntrospectionChild{Node: consumerAction, Relation: "sequence", Role: "left"},
		action.IntrospectionChild{Node: rightAct, Relation: "sequence", Role: "right"}), nil
}

func atomStreamParams(ctx context.Context, atom *Atom) map[string]any {
	params := ModifiersToMap(atom.Modifiers)
	if len(atom.Params) > 0 {
		if params == nil {
			params = make(map[string]any, len(atom.Params))
		}
		for key, value := range atom.Params {
			if _, exists := params[key]; !exists {
				params[key] = value
			}
		}
	}
	for key, value := range atom.ParamValues {
		if params == nil {
			params = make(map[string]any, len(atom.ParamValues))
		}
		params[key] = value
	}
	if len(atom.Args) > 0 {
		if params == nil {
			params = make(map[string]any, len(atom.Args))
		}
		for key, value := range atom.Args {
			if _, exists := params[key]; !exists {
				params[key] = resolveValue(ctx, value, nil)
			}
		}
	}
	return params
}

func buildStreamSource(
	ctx context.Context,
	bCtx *BuildContext,
	nodes []Expr,
) (action.AnyStreamAction, error) {
	if len(nodes) == 0 {
		return nil, xerr.Validation("stream segment is empty")
	}

	headAtom, ok := nodes[0].(*Atom)
	if !ok {
		return nil, xerr.Internal("stream segment must begin with an atom")
	}

	resolveConfigRefsInAtom(ctx, headAtom)
	resolveSymbolsInAtom(ctx, headAtom)

	src, ok := bCtx.Resolver.Stream(headAtom.Name)
	if !ok {
		return nil, xerr.NotFound(headAtom.Name + " is not a registered stream source")
	}

	headParams := atomStreamParams(ctx, headAtom)
	if headAtom.ConfigInject {
		cfg, _ := compileConfigFromCtx(ctx)
		var err error
		headParams, err = injectConfigIntoParams(headParams, src.ReqPayload(), cfg)
		if err != nil {
			return nil, xerr.Validation("stream source "+headAtom.Name+": "+err.Error(), err)
		}
	}
	if len(headParams) > 0 {
		src = &configuredSource{inner: src, cfg: headParams}
	}

	ops := make([]action.StreamOperator, 0, len(nodes)-1)
	for _, n := range nodes[1:] {
		atom, ok := n.(*Atom)
		if !ok {
			return nil, xerr.Internal("stream segment only accepts atoms after the head")
		}

		resolveConfigRefsInAtom(ctx, atom)
		resolveSymbolsInAtom(ctx, atom)

		named, ok := bCtx.Resolver.Operator(atom.Name)
		if !ok {
			return nil, xerr.NotFound(atom.Name + " is not a registered stream operator")
		}

		params := atomStreamParams(ctx, atom)
		if atom.ConfigInject {
			cfg, _ := compileConfigFromCtx(ctx)
			var err error
			params, err = injectConfigIntoParams(params, named.ConfigType, cfg)
			if err != nil {
				return nil, xerr.Validation("operator "+atom.Name+": "+err.Error(), err)
			}
		}

		op, err := named.Build(params)
		if err != nil {
			return nil, xerr.Validation("operator "+atom.Name+": "+err.Error(), err)
		}
		ops = append(ops, op)
	}

	return newPipelineStream(src.Describe().Name, src, ops), nil
}

func analyzeStreamSegment(resolver CapabilityResolver, nodes []Expr) error {
	if len(nodes) < 1 {
		return nil
	}
	previous, ok := contractForExpr(resolver, nodes[0])
	if !ok || !previous.Output.Stream {
		return nil
	}
	for _, node := range nodes[1:] {
		current, ok := contractForExpr(resolver, node)
		if !ok {
			continue
		}
		if err := requireCompatible(previous.Output, current.Input, previous.Name, current.Name); err != nil {
			return err
		}
		previous = current
	}
	return nil
}

type pipelineStream struct {
	name   string
	source action.AnyStreamAction
	ops    []action.StreamOperator
}

func newPipelineStream(name string, source action.AnyStreamAction, ops []action.StreamOperator) *pipelineStream {
	return &pipelineStream{name: name, source: source, ops: ops}
}

func (p *pipelineStream) Introspection() action.IntrospectionComposition {
	children := make([]action.IntrospectionChild, 0, len(p.ops)+1)
	if p.source != nil {
		meta := p.source.Describe()
		children = append(children, action.IntrospectionChild{
			Node:     &streamStage{meta: meta, kind: "stream_source"},
			Relation: "source", Role: "source",
		})
	}
	for _, op := range p.ops {
		meta := &action.Meta{Name: op.Name(), Tags: []string{"stream", "operator"}}
		children = append(children, action.IntrospectionChild{
			Node:     &streamStage{meta: meta, kind: "stream_operator"},
			Relation: "operator", Role: "operator", Label: op.Name(),
		})
	}
	return action.IntrospectionComposition{
		Kind: "stream_pipeline", Opaque: true,
		Notes:    []string{"stream source parameters and operator configuration values are omitted"},
		Children: children,
	}
}

type streamStage struct {
	meta *action.Meta
	kind string
}

func (s *streamStage) Describe() *action.Meta { return s.meta }

func (s *streamStage) Introspection() action.IntrospectionComposition {
	return action.IntrospectionComposition{Kind: s.kind, Opaque: true}
}

var (
	_ action.AnyStreamAction       = (*pipelineStream)(nil)
	_ action.IntrospectionProvider = (*pipelineStream)(nil)
)

func (p *pipelineStream) Describe() *action.Meta {
	return &action.Meta{Name: p.name, Description: "composed stream"}
}

func (p *pipelineStream) ReqPayload() any { return p.source.ReqPayload() }
func (p *pipelineStream) ResPayload() any { return p.source.ResPayload() }

func (p *pipelineStream) GetBindings() []action.Binding {
	return append([]action.Binding(nil), p.source.GetBindings()...)
}

func (p *pipelineStream) GetAnyHooks() []action.AnyHook {
	return append([]action.AnyHook(nil), p.source.GetAnyHooks()...)
}

func (p *pipelineStream) AddAnyHook(hooks ...action.AnyHook) {
	p.source.AddAnyHook(hooks...)
}

func (p *pipelineStream) CloneWithHooks(hooks ...action.AnyHook) action.AnyStreamAction {
	return &pipelineStream{
		name:   p.name,
		source: p.source.CloneWithHooks(hooks...),
		ops:    append([]action.StreamOperator(nil), p.ops...),
	}
}

func (p *pipelineStream) DoStreamAny(ctx context.Context, req any) (action.AnyStream, error) {
	up, err := p.source.DoStreamAny(ctx, req)
	if err != nil {
		return nil, err
	}
	for _, op := range p.ops {
		up, err = op.Apply(up)
		if err != nil {
			return nil, xerr.Internal("stream "+p.name+": operator "+op.Name()+": "+err.Error(), err)
		}
	}
	return up, nil
}

type configuredSource struct {
	inner action.AnyStreamAction
	cfg   map[string]any
}

func (c *configuredSource) Describe() *action.Meta         { return c.inner.Describe() }
func (c *configuredSource) ReqPayload() any                { return c.inner.ReqPayload() }
func (c *configuredSource) ResPayload() any                { return c.inner.ResPayload() }
func (c *configuredSource) GetBindings() []action.Binding  { return c.inner.GetBindings() }
func (c *configuredSource) GetAnyHooks() []action.AnyHook  { return c.inner.GetAnyHooks() }
func (c *configuredSource) AddAnyHook(h ...action.AnyHook) { c.inner.AddAnyHook(h...) }
func (c *configuredSource) CloneWithHooks(h ...action.AnyHook) action.AnyStreamAction {
	return &configuredSource{inner: c.inner.CloneWithHooks(h...), cfg: c.cfg}
}

func (c *configuredSource) DoStreamAny(ctx context.Context, req any) (action.AnyStream, error) {
	merged := make(map[string]any, len(c.cfg)+8)
	maps.Copy(merged, c.cfg)
	if req != nil {
		requestFields, err := configuredSourceRequestFields(req)
		if err != nil {
			return nil, xerr.Validation("configured stream source request: "+err.Error(), err)
		}
		maps.Copy(merged, requestFields)
	}
	return c.inner.DoStreamAny(ctx, merged)
}

func configuredSourceRequestFields(req any) (map[string]any, error) {
	if fields, ok := req.(map[string]any); ok {
		return fields, nil
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request %T: %w", req, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		return nil, fmt.Errorf("request type %T must encode as a JSON object: %w", req, err)
	}
	return fields, nil
}

func (p *ParallelExpr) Analyze(r CapabilityResolver) error {
	for _, child := range p.Branches {
		if err := child.Analyze(r); err != nil {
			return err
		}
	}
	return nil
}

func (p *ParallelExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	flat := flattenParallel(p.Branches)
	branches := make([]action.AnyAction, len(flat))
	for i, child := range flat {
		act, err := child.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
		branches[i] = act
	}
	disambiguateParallelBranchKeys(branches)
	program := action.ParallelAny("parallel", branches...).Build()
	if !bCtx.Introspect {
		return program, nil
	}
	children := make([]action.IntrospectionChild, 0, len(branches))
	for _, branch := range branches {
		label := ""
		if meta := branch.Describe(); meta != nil {
			label = meta.Name
		}
		children = append(children, action.IntrospectionChild{
			Node: branch, Relation: IntrospectionKindBranch, Role: IntrospectionKindBranch, Label: label,
		})
	}
	return action.WithComposition(program, action.IntrospectionComposition{
		Kind: "parallel", Children: children,
	}), nil
}

func disambiguateParallelBranchKeys(branches []action.AnyAction) {
	baseKeys := make([]string, len(branches))
	counts := make(map[string]int, len(branches))
	for i, branch := range branches {
		key := fmt.Sprintf("branch_%d", i)
		if meta := branch.Describe(); meta != nil && meta.Name != "" {
			key = meta.Name
		}
		baseKeys[i] = key
		counts[key]++
	}

	used := make(map[string]struct{}, len(branches))
	for _, key := range baseKeys {
		if counts[key] == 1 {
			used[key] = struct{}{}
		}
	}

	occurrences := make(map[string]int)
	for i, key := range baseKeys {
		if counts[key] == 1 {
			continue
		}
		occurrences[key]++
		label := fmt.Sprintf("%s#%d", key, occurrences[key])
		for {
			if _, exists := used[label]; !exists {
				break
			}
			label += "#"
		}
		used[label] = struct{}{}
		branches[i] = &parallelLabeledAction{AnyAction: branches[i], name: label}
	}
}

type parallelLabeledAction struct {
	action.AnyAction
	name string
}

func (a *parallelLabeledAction) Describe() *action.Meta {
	meta := &action.Meta{Name: a.name}
	if original := a.AnyAction.Describe(); original != nil {
		*meta = *original
		meta.Name = a.name
	}
	return meta
}

func (a *parallelLabeledAction) Children() []action.AnyAction {
	if composite, ok := a.AnyAction.(action.Composite); ok {
		return composite.Children()
	}
	return nil
}

func (a *parallelLabeledAction) CloneWithHooks(hooks ...action.AnyHook) action.AnyAction {
	return &parallelLabeledAction{
		AnyAction: a.AnyAction.CloneWithHooks(hooks...),
		name:      a.name,
	}
}

func flattenParallel(nodes []Expr) []Expr {
	var out []Expr
	for _, node := range nodes {
		if p, ok := node.(*ParallelExpr); ok {
			out = append(out, flattenParallel(p.Branches)...)
			continue
		}
		out = append(out, node)
	}
	return out
}

func (f *FallbackExpr) Analyze(r CapabilityResolver) error {
	if err := f.L.Analyze(r); err != nil {
		return err
	}
	return f.R.Analyze(r)
}

func (f *FallbackExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	left, err := f.L.Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}
	right, err := f.R.Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}
	return inspectBuiltPair(bCtx, action.FirstSuccessAny("fallback", left, right).Build(), "fallback",
		action.IntrospectionChild{Node: left, Relation: "candidate", Role: "primary"},
		action.IntrospectionChild{Node: right, Relation: "candidate", Role: "fallback"}), nil
}

func (*ProjectionExpr) Analyze(CapabilityResolver) error { return nil }

func (p *ProjectionExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	return (&Atom{
		Name:   "projection.project",
		Params: map[string]string{"raw": p.Raw},
		Pos:    p.Pos,
	}).Build(ctx, bCtx)
}

func (c *ConditionalExpr) Analyze(r CapabilityResolver) error {
	if err := c.Cond.Analyze(r); err != nil {
		return err
	}
	if err := c.Then.Analyze(r); err != nil {
		return err
	}
	if c.Else != nil {
		return c.Else.Analyze(r)
	}
	return nil
}

func (c *ConditionalExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	cond, err := c.Cond.Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}
	thenBranch, err := c.Then.Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}
	var elseBranch action.AnyAction
	if c.Else != nil {
		elseBranch, err = c.Else.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
	}
	program := action.TernaryAny("conditional", cond, thenBranch, elseBranch).Build()
	if !bCtx.Introspect {
		return program, nil
	}
	children := []action.IntrospectionChild{
		{Node: cond, Relation: "predicate", Role: "condition"},
		{Node: thenBranch, Relation: "branch", Role: "then"},
	}
	composition := action.IntrospectionComposition{Kind: "conditional", Children: children}
	if elseBranch != nil {
		composition.Children = append(composition.Children, action.IntrospectionChild{
			Node: elseBranch, Relation: "branch", Role: "else",
		})
	} else {
		composition.Notes = []string{"else branch is absent; a false condition returns nil"}
	}
	return action.WithComposition(program, composition), nil
}

func (l *LoopExpr) Analyze(r CapabilityResolver) error {
	return l.Body.Analyze(r)
}

func (l *LoopExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	body, err := l.Body.Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}
	compiledCondition := PreprocessDots(l.Until)
	prog, err := CompileExpression(compiledCondition, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, xerr.Validation("loop: invalid condition "+l.Until, err)
	}
	until := func(out any) bool {
		val, runErr := expr.Run(prog, MergeSymbolNamespaces(ctx, BuildEnv(out)))
		if runErr != nil {
			return false
		}
		satisfied, ok := val.(bool)
		return ok && satisfied
	}
	program := action.LoopAny("loop", body, until, 15).Build()
	if !bCtx.Introspect {
		return program, nil
	}
	return action.WithComposition(program, action.IntrospectionComposition{
		Kind: "loop", Opaque: true,
		Notes: []string{"termination predicate is closure-captured and omitted"},
		Children: []action.IntrospectionChild{{
			Node: body, Relation: "body", Role: "body",
		}},
	}), nil
}

func (*AssertExpr) Analyze(CapabilityResolver) error { return nil }

func (a *AssertExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	cleanCondition := PreprocessDots(a.Condition)
	prog, err := CompileExpression(cleanCondition, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, xerr.Validation("assert: invalid expression "+a.Condition, err)
	}
	predicate := func(input any) bool {
		val, runErr := expr.Run(prog, MergeSymbolNamespaces(ctx, BuildEnv(input)))
		if runErr != nil {
			return false
		}
		passed, ok := val.(bool)
		return ok && passed
	}
	return inspectBuiltComposition(bCtx, action.AssertAny("assert", predicate, a.Message).Build(),
		action.IntrospectionComposition{
			Kind: "assertion", Opaque: true,
			Notes: []string{"assertion predicate is closure-captured and omitted"},
		}), nil
}

func resolveConfigRefsInAtom(ctx context.Context, a *Atom) {
	cfg, cliArgs := compileConfigFromCtx(ctx)
	for key, value := range a.Params {
		a.Params[key] = resolveConfigString(cfg, cliArgs, value)
	}
	for _, value := range a.Args {
		resolveConfigRefsInValue(cfg, cliArgs, value)
	}
	for i, raw := range a.Modifiers {
		a.Modifiers[i] = resolveConfigRefsInModifier(cfg, cliArgs, raw)
		if i < len(a.ModifierValues) {
			a.ModifierValues[i].Raw = a.Modifiers[i]
		}
	}
}

func resolveConfigRefsInModifier(cfg map[string]string, cliArgs []string, raw string) string {
	eq := strings.IndexByte(raw, '=')
	if eq <= 0 {
		return raw
	}
	value := raw[eq+1:]
	resolved := resolveConfigString(cfg, cliArgs, value)
	if resolved == value {
		return raw
	}
	return raw[:eq+1] + resolved
}

func hasInjections(a *Atom) bool {
	return len(a.Params) > 0 || len(a.Args) > 0 ||
		a.Prompt != "" || len(a.Targets) > 0 || len(a.Excludes) > 0
}

func wrapWithInjections(inner action.AnyAction, a *Atom) action.AnyAction {
	pos := a.Pos
	return action.New(a.Name, func(ctx context.Context, in any) (any, error) {
		payload := buildPayload(ctx, in, a)
		res, err := action.InvokeAny(ctx, inner, payload)
		if err != nil {
			// Attach pos to the error when not already tagged with that file.
			if pos.File != "" && !strings.Contains(err.Error(), pos.File+":") {
				return nil, SourceError(pos, "%w", err)
			}
			return nil, err
		}
		return res, nil
	}).Build()
}

func resolveConfigString(cfg map[string]string, cliArgs []string, value string) string {
	// Guard: only parse strings that declare an @ reference. Never touch raw projections or data.
	if !strings.HasPrefix(value, "@") {
		return value
	}

	// 1. Fallback operator: @flag.name ?? "default" or @arg.1 ?? 8080
	if left, right, ok := strings.Cut(value, "??"); ok {
		resolved := resolveConfigString(cfg, cliArgs, strings.TrimSpace(left))
		if resolved != "" {
			return resolved
		}
		return resolveConfigString(cfg, cliArgs, TrimQuotes(strings.TrimSpace(right)))
	}

	// 2. Compile-time configuration reference
	if key, ok := strings.CutPrefix(value, "@config."); ok {
		return cfg[key]
	}

	// 3. Command-line flag reference (--name=val or -name=val)
	if name, ok := strings.CutPrefix(value, "@flag."); ok {
		for _, arg := range cliArgs {
			for _, prefix := range []string{"--" + name + "=", "-" + name + "="} {
				if resolved, found := strings.CutPrefix(arg, prefix); found {
					return resolved
				}
			}
		}
		return ""
	}

	// 4. Positional CLI argument (@arg.1, @arg.2)
	if idxStr, ok := strings.CutPrefix(value, "@arg."); ok {
		idx, err := strconv.Atoi(idxStr)
		if err == nil && idx >= 1 {
			var positional []string
			for _, arg := range cliArgs {
				if !strings.HasPrefix(arg, "-") {
					positional = append(positional, arg)
				}
			}
			if idx <= len(positional) {
				return positional[idx-1]
			}
		}
		return ""
	}

	return value
}

func resolveConfigRefsInValue(cfg map[string]string, cliArgs []string, value *Value) {
	if value == nil {
		return
	}
	switch value.Kind {
	case ValueString:
		// Explicit string stays string (preserves quotes in "@config.retries" -> "3")
		value.Str = resolveConfigString(cfg, cliArgs, value.Str)
	case ValueRef:
		if strings.HasPrefix(value.Ref, "config.") || strings.HasPrefix(value.Ref, "flag.") {
			value.Kind = ValueString
			value.Str = resolveConfigString(cfg, cliArgs, "@"+value.Ref)
			value.Ref = ""
		} else if strings.HasPrefix(value.Ref, "arg.") {
			// Unquoted positional arguments (@arg.1) auto-coerce to number if numeric.
			resolved := resolveConfigString(cfg, cliArgs, "@"+value.Ref)
			if n, err := strconv.ParseInt(resolved, 10, 64); err == nil {
				value.Kind = ValueNumber
				value.Num = float64(n)
			} else {
				value.Kind = ValueString
				value.Str = resolved
			}
		}
	case ValueMap:
		for i := range value.Map {
			resolveConfigRefsInValue(cfg, cliArgs, value.Map[i].Value)
		}
	case ValueSlice:
		for _, item := range value.Slice {
			resolveConfigRefsInValue(cfg, cliArgs, item)
		}
	case ValueNumber, ValueBool, ValueNull, ValueBare, ValueTyped:
	}
}

func buildPayload(ctx context.Context, in any, a *Atom) map[string]any {
	m, _ := in.(map[string]any)

	// runtime.const is a generator: it resolves references against input m (e.g. .id),
	// but emits ONLY its own declared parameters/arguments without echoing unrequested keys
	if a.Name == "runtime.const" {
		out := make(map[string]any, len(a.Params)+len(a.Args))
		for k, v := range a.Params {
			out[k] = v
		}
		maps.Copy(out, a.ParamValues)
		for k, v := range a.Args {
			out[k] = resolveValue(ctx, v, m)
		}
		return out
	}

	merged := make(map[string]any, len(m)+len(a.Params)+len(a.Args)+4)
	maps.Copy(merged, m)
	if m == nil && in != nil {
		merged["__root__"] = in
	}
	for k, v := range a.Params {
		merged[k] = v
	}
	maps.Copy(merged, a.ParamValues)
	for k, v := range a.Args {
		merged[k] = resolveValue(ctx, v, m)
	}
	if a.Prompt != "" {
		merged["prompt"] = a.Prompt
	}
	if len(a.Targets) > 0 {
		merged["targets"] = a.Targets
	}
	if len(a.Excludes) > 0 {
		merged["excludes"] = a.Excludes
	}
	return merged
}

func resolveValue(ctx context.Context, v *Value, state map[string]any) any {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case ValueString:
		return v.Str
	case ValueTyped:
		return v.Typed
	case ValueBare:
		if val, ok := lookupSymbolValue(ctx, v.Str); ok {
			return RuntimeSymbolValue(val)
		}
		return v.Str
	case ValueNumber:
		return v.Num
	case ValueBool:
		return v.Bool
	case ValueNull:
		return nil
	case ValueRef:
		if v.Ref == "" {
			return state
		}
		val, _ := lookupPath(state, v.Ref)
		return val
	case ValueMap:
		out := make(map[string]any, len(v.Map))
		for _, e := range v.Map {
			out[e.Key] = resolveValue(ctx, e.Value, state)
		}
		return out
	case ValueSlice:
		out := make([]any, len(v.Slice))
		for i, e := range v.Slice {
			out[i] = resolveValue(ctx, e, state)
		}
		return out
	}
	return nil
}

func resolveSymbolsInAtom(ctx context.Context, a *Atom) {
	for key, value := range a.Params {
		if val, ok := lookupSymbolValue(ctx, value); ok {
			if typed, ok := val.(TypedSymbolValue); ok {
				if a.ParamValues == nil {
					a.ParamValues = make(map[string]any)
				}
				a.ParamValues[key] = typed.Value
				continue
			}
			a.Params[key] = fmt.Sprint(val)
		}
	}
	for _, value := range a.Args {
		resolveSymbolsInValue(ctx, value)
	}
	a.ModifierValues = resolveTypedModifierValues(ctx, a.Modifiers, a.ModifierValues)
}

func resolveTypedModifierValues(ctx context.Context, raws []string, values []ModifierValue) []ModifierValue {
	out := make([]ModifierValue, len(raws))
	copy(out, values)
	for i, raw := range raws {
		if i >= len(values) {
			out[i] = ModifierValue{Raw: raw}
		} else {
			out[i].Raw = raw
		}
		input := &out[i]
		if input.Typed || input.Quoted || !strings.Contains(raw, "=") {
			continue
		}
		name, symbol := splitRawModifier(raw)
		if value, ok := lookupSymbolValue(ctx, symbol); ok {
			if typed, ok := value.(TypedSymbolValue); ok {
				input.Value = typed.Value
				input.Typed = true
				input.Raw = name + "=" + symbol
			}
		}
	}
	return out
}

func resolveSymbolsInValue(ctx context.Context, value *Value) {
	if value == nil {
		return
	}
	switch value.Kind {
	case ValueBare:
		if val, ok := lookupSymbolValue(ctx, value.Str); ok {
			if typed, ok := val.(TypedSymbolValue); ok {
				switch native := typed.Value.(type) {
				case string:
					value.Kind = ValueString
					value.Str = native
				default:
					value.Kind = ValueTyped
					value.Typed = native
				}
				return
			}
			switch v := val.(type) {
			case string:
				value.Kind = ValueString
				value.Str = v
			case float64:
				value.Kind = ValueNumber
				value.Num = v
			case int:
				value.Kind = ValueNumber
				value.Num = float64(v)
			case int64:
				value.Kind = ValueNumber
				value.Num = float64(v)
			case bool:
				value.Kind = ValueBool
				value.Bool = v
			case xfile.Ref:
				value.Kind = ValueTyped
				value.Typed = v
			default:
				value.Kind = ValueTyped
				value.Typed = val
			}
		}
	case ValueMap:
		for i := range value.Map {
			resolveSymbolsInValue(ctx, value.Map[i].Value)
		}
	case ValueSlice:
		for _, item := range value.Slice {
			resolveSymbolsInValue(ctx, item)
		}
	case ValueString, ValueNumber, ValueBool, ValueNull, ValueRef, ValueTyped:
	}
}

func lookupSymbolValue(ctx context.Context, raw string) (any, bool) {
	ns, name, ok := strings.Cut(raw, ".")
	if !ok {
		return nil, false
	}
	if ctx != nil {
		if local := LocalSymbolsFrom(ctx); local != nil {
			if localNS, exists := local[ns]; exists {
				return lookupPath(localNS.Symbols, name)
			}
		}
	}
	return LookupSymbol(ns, name)
}

func lookupPath(m map[string]any, path string) (any, bool) {
	if path == "" {
		return m, true
	}
	var cur any = m
	remaining := path
	for remaining != "" {
		var part string
		if dot := strings.IndexByte(remaining, '.'); dot >= 0 {
			part, remaining = remaining[:dot], remaining[dot+1:]
		} else {
			part, remaining = remaining, ""
		}
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mm[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func BuildEnv(input any) map[string]any {
	if m, ok := input.(map[string]any); ok {
		env := make(map[string]any, len(m)+1)
		maps.Copy(env, m)
		env["__root__"] = m
		return env
	}
	return map[string]any{"__root__": input, "result": input}
}

func PreprocessDots(src string) string {
	var out strings.Builder
	out.Grow(len(src))

	var quote byte
	for i, n := 0, len(src); i < n; {
		c := src[i]

		if quote != 0 {
			out.WriteByte(c)
			if c == '\\' && i+1 < n {
				out.WriteByte(src[i+1])
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
			continue
		}

		switch c {
		case '"', '\'', '`':
			quote = c
			out.WriteByte(c)
			i++
		case '.':
			if i > 0 && (isDotMemberAccess(src[i-1]) || src[i-1] == '?') {
				out.WriteByte(c)
				i++
				continue
			}
			if i+1 < n && isDotIdentStart(src[i+1]) {
				end := i + 2
				for end < n && (isDotIdentStart(src[end]) || src[end] >= '0' && src[end] <= '9') {
					end++
				}
				// Use the installed expr version's registry, not a drifting hand-written list.
				// Ordinary fields retain the established .name -> name rewrite.
				if dotFieldNeedsRoot(src[i+1 : end]) {
					out.WriteString("__root__.")
				}
				i++
				continue
			}
			out.WriteString("__root__")
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func dotFieldNeedsRoot(name string) bool {
	if _, collides := builtin.Index[name]; collides {
		return true
	}
	// expr's literal/operator keywords cannot safely become bare identifiers.
	// __root__ is Flow's own environment binding, not the user's field.
	switch name {
	case "true", "false", "nil", "not", "in", "or", "and", "matches", "contains",
		"startsWith", "endsWith", "let", "if", "else", "__root__":
		return true
	}
	return false
}

func isDotMemberAccess(prev byte) bool {
	switch {
	case prev >= 'a' && prev <= 'z',
		prev >= 'A' && prev <= 'Z',
		prev >= '0' && prev <= '9':
		return true
	case prev == '_', prev == ')', prev == ']', prev == '#', prev == '@':
		return true
	}
	return false
}

func isDotIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func injectConfigIntoParams(params map[string]any, target any, cfg map[string]string) (map[string]any, error) {
	if len(cfg) == 0 || target == nil {
		return params, nil
	}
	typ, ok := target.(reflect.Type)
	if !ok {
		typ = reflect.TypeOf(target)
	}
	if typ == nil {
		return params, nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return params, nil
	}
	if params == nil {
		params = make(map[string]any, typ.NumField())
	}
	for field := range typ.Fields() {
		if !field.IsExported() {
			continue
		}
		jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if jsonName == "-" {
			continue
		}
		if jsonName == "" {
			jsonName = field.Name
		}
		if _, exists := params[jsonName]; exists {
			continue
		}
		raw, ok := cfg[jsonName]
		if !ok {
			continue
		}
		value, err := action.CoerceStringValue(raw, field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", jsonName, err)
		}
		params[jsonName] = value
	}
	return params, nil
}

func (d *DecoratedExpr) Analyze(r CapabilityResolver) error {
	return d.Inner.Analyze(r)
}

func (d *DecoratedExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	inner, err := d.Inner.Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}
	if bCtx.Modifiers == nil || len(d.Modifiers) == 0 {
		return inner, nil
	}
	cfg, cliArgs := compileConfigFromCtx(ctx)
	modifiers := resolveConfigRefsInModifiers(cfg, cliArgs, d.Modifiers)
	values := resolveTypedModifierValues(ctx, modifiers, d.ModifierValues)
	modified, err := bCtx.Modifiers.ApplyAllValues(inner, values)
	if err != nil {
		if hasTypedModifierValues(values) {
			return nil, SourceError(d.Pos, "%w", err)
		}
		return nil, err
	}
	return inspectBuiltChild(bCtx, modified, "modifier", action.IntrospectionChild{
		Node: inner, Relation: IntrospectionKindWraps, Role: IntrospectionKindAction,
	}), nil
}

func runAtomAdvisers(a *Atom, act action.AnyAction, bCtx *BuildContext, advisers []AtomAdviseFunc) (action.AnyAction, error) {
	if len(advisers) == 0 {
		return act, nil
	}
	original := act
	builder := action.Dynamic(act)
	modifiersBefore := slices.Clone(a.Modifiers)
	var ranAdvisor bool
	for _, advise := range advisers {
		if advise == nil {
			continue
		}
		ranAdvisor = true
		if err := advise(a, builder); err != nil {
			return nil, xerr.Validation("atom "+a.Name+" advisor: "+err.Error(), err)
		}
	}
	if !ranAdvisor {
		return act, nil
	}
	// Advisers may mutate the AST and/or add Kernel builder middleware. Always
	// materialize the builder when an adviser runs; otherwise middleware-only
	// policies are silently discarded. Preserve the existing introspection
	// shape unless an adviser actually consumed or changed source modifiers.
	act = builder.Build()
	if composition, ok := original.(action.IntrospectionProvider); ok {
		act = action.WithComposition(act, composition.Introspection())
	}
	if !slices.Equal(modifiersBefore, a.Modifiers) {
		act = inspectBuiltChild(bCtx, act, "advisor", action.IntrospectionChild{
			Node: original, Relation: "wraps", Role: IntrospectionKindAction,
		})
	}
	return act, nil
}

func finalizeAtomAction(a *Atom, bCtx *BuildContext, act action.AnyAction) (action.AnyAction, error) {
	injected := hasInjections(a)
	if injected {
		inner := act
		act = inspectBuiltChild(bCtx, wrapWithInjections(inner, a), "injection", action.IntrospectionChild{
			Node: inner, Relation: IntrospectionKindWraps, Role: IntrospectionKindAction,
		})
	}
	if bCtx.Modifiers == nil || len(a.Modifiers) == 0 {
		if injected {
			return act, nil
		}
		return inspectBuiltAction(bCtx, act), nil
	}
	modified, err := bCtx.Modifiers.ApplyAllValues(act, a.ModifierValues)
	if err != nil {
		if hasTypedModifierValues(a.ModifierValues) {
			return nil, SourceError(a.Pos, "%w", err)
		}
		return nil, err
	}
	return inspectBuiltChild(bCtx, modified, "modifier", action.IntrospectionChild{
		Node: act, Relation: "wraps", Role: IntrospectionKindAction,
	}), nil
}

func hasTypedModifierValues(values []ModifierValue) bool {
	for _, value := range values {
		if value.Typed {
			return true
		}
	}
	return false
}
