package core

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// ── Atom ─────────────────────────────────────────────────────────────

func (*Atom) Analyze(CapabilityResolver) error { return nil }

func (a *Atom) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	if a == nil || strings.TrimSpace(a.Name) == "" {
		return nil, xerr.Validation("atom name is empty")
	}
	if bCtx.Resolver == nil {
		return nil, xerr.Internal("build: capability resolver is nil")
	}
	resolveConfigRefsInAtom(ctx, a)

	act, ok := bCtx.Resolver.Action(a.Name)
	if !ok {
		return nil, xerr.NotFound("capability " + a.Name + " not registered")
	}

	if len(bCtx.Advisers) > 0 {
		builder := action.Dynamic(act)
		modifiersBefore := slices.Clone(a.Modifiers)
		for _, advise := range bCtx.Advisers {
			if advise == nil {
				continue
			}
			if err := advise(a, builder); err != nil {
				return nil, xerr.Validation("atom "+a.Name+" advisor: "+err.Error(), err)
			}
		}
		// Materialize the dynamic wrapper only when an advisor actually
		// consumed a modifier. Otherwise the wrapper would invoke the
		// original action through InvokeAny, adding a second
		// "action <name> execution failed:" prefix to every error.
		if !slices.Equal(modifiersBefore, a.Modifiers) {
			act = builder.Build()
		}
	}

	strict := (strictFromCtx(ctx) || hasModifier(a.Modifiers, "strict")) &&
		!hasModifier(a.Modifiers, "lenient")

	if strict {
		if typed, ok := act.(action.TypedPayload); ok && typed.ReqPayload() != nil {
			act = act.CloneWithHooks(strictHook())
		}
	}

	if hasInjections(a) {
		act = wrapWithInjections(act, a)
	}

	if bCtx.Modifiers == nil || len(a.Modifiers) == 0 {
		return act, nil
	}
	return bCtx.Modifiers.ApplyAll(act, a.Modifiers)
}

// ── PipeExpr ─────────────────────────────────────────────────────────

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
		return ComposedPipe(left, right), nil
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
		return action.Dynamic(consumer).Build(), nil
	}

	rightAct, err := rebuildPipe(rightNodes).Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}

	return ComposedPipe(consumer, rightAct), nil
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

	src, ok := bCtx.Resolver.Stream(headAtom.Name)
	if !ok {
		return nil, xerr.NotFound(headAtom.Name + " is not a registered stream source")
	}

	headParams := ModifiersToMap(headAtom.Modifiers)
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

		named, ok := bCtx.Resolver.Operator(atom.Name)
		if !ok {
			return nil, xerr.NotFound(atom.Name + " is not a registered stream operator")
		}

		params := ModifiersToMap(atom.Modifiers)
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

// ── Stream Source Wrappers ───────────────────────────────────────────

type pipelineStream struct {
	name   string
	source action.AnyStreamAction
	ops    []action.StreamOperator
}

func newPipelineStream(name string, source action.AnyStreamAction, ops []action.StreamOperator) *pipelineStream {
	return &pipelineStream{name: name, source: source, ops: ops}
}

var _ action.AnyStreamAction = (*pipelineStream)(nil)

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
	if reqMap, ok := req.(map[string]any); ok {
		maps.Copy(merged, reqMap)
	}
	return c.inner.DoStreamAny(ctx, merged)
}

// ── ParallelExpr ─────────────────────────────────────────────────────

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
	return action.ParallelAny("parallel", branches...).Build(), nil
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

// ── FallbackExpr ─────────────────────────────────────────────────────

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
	return action.FirstSuccessAny("fallback", left, right).Build(), nil
}

// ── ProjectionExpr ───────────────────────────────────────────────────

func (*ProjectionExpr) Analyze(CapabilityResolver) error { return nil }

func (p *ProjectionExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	return (&Atom{
		Name:   "projection.project",
		Params: map[string]string{"raw": p.Raw},
		Pos:    p.Pos,
	}).Build(ctx, bCtx)
}

// ── ConditionalExpr ──────────────────────────────────────────────────

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
	return action.TernaryAny("conditional", cond, thenBranch, elseBranch).Build(), nil
}

// ── LoopExpr ─────────────────────────────────────────────────────────

func (l *LoopExpr) Analyze(r CapabilityResolver) error {
	return l.Body.Analyze(r)
}

func (l *LoopExpr) Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	body, err := l.Body.Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}
	compiledCondition := PreprocessDots(l.Until)
	prog, err := expr.Compile(compiledCondition, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, xerr.Validation("loop: invalid condition "+l.Until, err)
	}
	until := func(out any) bool {
		val, runErr := expr.Run(prog, BuildEnv(out))
		if runErr != nil {
			return false
		}
		satisfied, ok := val.(bool)
		return ok && satisfied
	}
	return action.LoopAny("loop", body, until, 15).Build(), nil
}

// ── AssertExpr ───────────────────────────────────────────────────────

func (*AssertExpr) Analyze(CapabilityResolver) error { return nil }

func (a *AssertExpr) Build(_ context.Context, _ *BuildContext) (action.AnyAction, error) {
	cleanCondition := PreprocessDots(a.Condition)
	prog, err := expr.Compile(cleanCondition, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, xerr.Validation("assert: invalid expression "+a.Condition, err)
	}
	predicate := func(input any) bool {
		val, runErr := expr.Run(prog, BuildEnv(input))
		if runErr != nil {
			return false
		}
		passed, ok := val.(bool)
		return ok && passed
	}
	return action.AssertAny("assert", predicate, a.Message).Build(), nil
}

// ── Atom-only helpers ────────────────────────────────────────────────

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
	}
}

// resolveConfigRefsInModifier rewrites @config.X and @flag.X references
// inside a raw modifier string of the form name=value. Modifiers
// without a value or with an unrecognized reference are returned
// unchanged.
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

func resolveConfigRefsInValue(cfg map[string]string, cliArgs []string, value *Value) {
	if value == nil {
		return
	}
	switch value.Kind {
	case ValueString:
		value.Str = resolveConfigString(cfg, cliArgs, value.Str)
	case ValueMap:
		for i := range value.Map {
			resolveConfigRefsInValue(cfg, cliArgs, value.Map[i].Value)
		}
	case ValueSlice:
		for _, item := range value.Slice {
			resolveConfigRefsInValue(cfg, cliArgs, item)
		}
	case ValueNumber, ValueBool, ValueNull, ValueRef, ValueBare:
	}
}

func resolveConfigString(cfg map[string]string, cliArgs []string, value string) string {
	if key, ok := strings.CutPrefix(value, "@config."); ok {
		return cfg[key]
	}
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
	return value
}

func hasInjections(a *Atom) bool {
	return len(a.Params) > 0 || len(a.Args) > 0 ||
		a.Prompt != "" || len(a.Targets) > 0 || len(a.Excludes) > 0
}

func wrapWithInjections(inner action.AnyAction, a *Atom) action.AnyAction {
	pos := a.Pos
	return action.New(a.Name, func(ctx context.Context, in any) (any, error) {
		payload := buildPayload(in, a)
		res, err := action.InvokeAny(ctx, inner, payload)
		if err != nil {
			// Jeśli błąd nie ma jeszcze prefiksu pliku i linii, dodaj klikalny link
			if pos.File != "" && !strings.Contains(err.Error(), pos.File+":") {
				return nil, SourceError(pos, "%w", err)
			}
			return nil, err
		}
		return res, nil
	}).Build()
}

func buildPayload(in any, a *Atom) map[string]any {
	m, _ := in.(map[string]any)
	merged := make(map[string]any, len(m)+len(a.Params)+len(a.Args)+4)
	maps.Copy(merged, m)
	if m == nil && in != nil {
		merged["__root__"] = in
	}
	for k, v := range a.Params {
		merged[k] = v
	}
	for k, v := range a.Args {
		merged[k] = resolveValue(v, m)
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

func resolveValue(v *Value, state map[string]any) any {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case ValueString:
		return v.Str
	case ValueBare:
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
			out[e.Key] = resolveValue(e.Value, state)
		}
		return out
	case ValueSlice:
		out := make([]any, len(v.Slice))
		for i, e := range v.Slice {
			out[i] = resolveValue(e, state)
		}
		return out
	}
	return nil
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

// PreprocessDots rewrites a user-authored expression for expr-lang:
// leading-dot references become bare identifiers (`.name` → `name`),
// a bare `.` becomes `__root__`, and `.` after an identifier, `)`, `]`,
// `#`, or `@` is left alone as member access.
//
// Quoted strings (`"`, `'`, “ ` “) are copied verbatim; backslash
// escapes inside them are honored, so a `.` inside `"a.b"` survives
// untouched. This is the single implementation used by assert, loop,
// match, @on_error, and projection — no other package should implement
// its own dot rewriting.
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
			if i > 0 && isDotMemberAccess(src[i-1]) {
				out.WriteByte(c)
				i++
				continue
			}
			if i+1 < n && isDotIdentStart(src[i+1]) {
				i++ // drop the dot; the identifier itself follows
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

// isDotMemberAccess reports whether prev, the byte immediately before a
// `.`, indicates that the `.` is member access on the preceding token
// (`foo.bar`, `arr[0].field`, `#.name`) rather than a root reference.
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

// isDotIdentStart reports whether c may start an identifier. Used to
// decide whether `.foo` is a root reference (yes) or a numeric literal
// continuation `.5` (no).
func isDotIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// injectConfigIntoParams copies matching @config values into params,
// coercing each value to the target field type. Existing params
// (explicit :mod= values) win over injected config. target is either
// an operator's ConfigType or a stream source's request payload type;
// a nil target or a non-struct target is a no-op.
func injectConfigIntoParams(params map[string]any, target any, cfg map[string]string) (map[string]any, error) {
	if len(cfg) == 0 || target == nil {
		return params, nil
	}
	typ := reflect.TypeOf(target)
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
