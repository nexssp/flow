package projection

import (
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

type expressionRootViewSelection struct {
	topLevelFields map[string]struct{}
	nestedFields   map[string]*expressionRootViewSelection
}

type projectionRootPathPlan struct {
	safe  bool
	paths [][]string
}

// rootViewSelection returns a partial current-root view only when the generated
// expression uses statically named current-root paths. The root map itself keeps
// every key; only unreferenced values are left unconverted.
func (cache *programCache) rootViewSelection(
	raw string,
	visibleNames map[string]struct{},
	pipelineInputNames map[string]struct{},
	rootBindings map[string]struct{},
	root map[string]any,
) (*expressionRootViewSelection, bool) {
	cacheKey := projectionRootPathPlanCacheKey(raw, visibleNames, pipelineInputNames, rootBindings)
	planValue, exists := cache.rootPathPlans.Load(cacheKey)
	if !exists {
		source, err := buildProgramSourceWithVisibleNames(raw, visibleNames, pipelineInputNames)
		plan := projectionRootPathPlan{}
		if err == nil {
			plan.paths, plan.safe = analyzeProjectionCurrentRootPaths(source, rootBindings)
		}
		planValue, _ = cache.rootPathPlans.LoadOrStore(cacheKey, plan)
	}
	plan, ok := planValue.(projectionRootPathPlan)
	if !ok || !plan.safe {
		return nil, false
	}

	selected := &expressionRootViewSelection{
		topLevelFields: make(map[string]struct{}, len(plan.paths)),
	}
	for _, path := range plan.paths {
		if len(path) == 0 {
			return nil, false
		}
		field, exists, safe := expressionRootMapFieldForStaticPath(root, path[0])
		if !safe {
			return nil, false
		}
		if exists {
			selected.topLevelFields[field] = struct{}{}
			var nested *expressionRootViewSelection
			if len(path) > 1 {
				nested = expressionNestedRootViewSelection(root[field], path[1:])
			}
			if selected.nestedFields == nil {
				selected.nestedFields = make(map[string]*expressionRootViewSelection)
			}
			if previous, exists := selected.nestedFields[field]; exists {
				selected.nestedFields[field] = mergeExpressionRootViewSelections(previous, nested)
			} else {
				selected.nestedFields[field] = nested
			}
		}
	}
	return selected, true
}

// expressionNestedRootViewSelection limits recursive conversion only through
// exact-key map paths. Aliases and non-map values use the established full view.
func expressionNestedRootViewSelection(value any, path []string) *expressionRootViewSelection {
	if len(path) == 0 {
		return nil
	}
	current, ok := value.(map[string]any)
	if !ok || current == nil {
		return nil
	}
	field, exists, safe := expressionRootMapFieldForStaticPath(current, path[0])
	if !safe {
		return nil
	}
	selection := &expressionRootViewSelection{topLevelFields: make(map[string]struct{})}
	if !exists {
		return selection
	}
	selection.topLevelFields[field] = struct{}{}
	selection.nestedFields = map[string]*expressionRootViewSelection{
		field: expressionNestedRootViewSelection(current[field], path[1:]),
	}
	return selection
}

func mergeExpressionRootViewSelections(left, right *expressionRootViewSelection) *expressionRootViewSelection {
	if left == nil || right == nil {
		return nil
	}
	if len(right.topLevelFields) > 0 && left.nestedFields == nil {
		left.nestedFields = make(map[string]*expressionRootViewSelection)
	}
	for field := range right.topLevelFields {
		if _, exists := left.topLevelFields[field]; !exists {
			left.topLevelFields[field] = struct{}{}
			left.nestedFields[field] = right.nestedFields[field]
			continue
		}
		left.nestedFields[field] = mergeExpressionRootViewSelections(left.nestedFields[field], right.nestedFields[field])
	}
	return left
}

func projectionRootPathPlanCacheKey(raw string, visibleNames, pipelineInputNames, rootBindings map[string]struct{}) string {
	bindings := make([]string, 0, len(rootBindings))
	for name := range rootBindings {
		bindings = append(bindings, name)
	}
	sort.Strings(bindings)
	return projectionProgramCacheKey(raw, visibleNames, pipelineInputNames) + "\x00root-bindings\x00" + strings.Join(bindings, "\x00")
}

func analyzeProjectionCurrentRootPaths(source string, rootBindings map[string]struct{}) ([][]string, bool) {
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, false
	}
	analyzer := projectionCurrentRootPathAnalyzer{rootBindings: rootBindings, safe: true}
	analyzer.visit(tree.Node)
	return analyzer.paths, analyzer.safe
}

type projectionCurrentRootPathAnalyzer struct {
	rootBindings map[string]struct{}
	paths        [][]string
	safe         bool
}

func (analyzer *projectionCurrentRootPathAnalyzer) visit(node ast.Node) {
	if !analyzer.safe || node == nil {
		return
	}
	if analyzer.visitCurrentRootPath(node) {
		return
	}
	analyzer.visitChildren(node)
}

func (analyzer *projectionCurrentRootPathAnalyzer) visitCurrentRootPath(node ast.Node) bool {
	if path, ok := projectionCurrentRootMemberPath(node, analyzer.rootBindings); ok {
		if len(path) == 0 {
			analyzer.safe = false
			return true
		}
		analyzer.paths = append(analyzer.paths, path)
		return true
	}
	return false
}

func (analyzer *projectionCurrentRootPathAnalyzer) visitChildren(node ast.Node) {
	switch current := node.(type) {
	case *ast.IdentifierNode:
		analyzer.visitIdentifier(current)
	case *ast.MemberNode:
		analyzer.visitMember(current)
	case *ast.ChainNode:
		analyzer.visit(current.Node)
	case *ast.UnaryNode:
		analyzer.visit(current.Node)
	case *ast.BinaryNode:
		analyzer.visit(current.Left)
		analyzer.visit(current.Right)
	case *ast.SliceNode:
		analyzer.visit(current.Node)
		analyzer.visit(current.From)
		analyzer.visit(current.To)
	case *ast.CallNode:
		analyzer.visit(current.Callee)
		visitProjectionASTNodes(analyzer, current.Arguments)
	case *ast.BuiltinNode:
		visitProjectionASTNodes(analyzer, current.Arguments)
		analyzer.visit(current.Map)
	case *ast.PredicateNode:
		analyzer.visit(current.Node)
	case *ast.VariableDeclaratorNode:
		analyzer.visit(current.Value)
		analyzer.visit(current.Expr)
	case *ast.SequenceNode:
		visitProjectionASTNodes(analyzer, current.Nodes)
	case *ast.ConditionalNode:
		analyzer.visit(current.Cond)
		analyzer.visit(current.Exp1)
		analyzer.visit(current.Exp2)
	case *ast.ArrayNode:
		visitProjectionASTNodes(analyzer, current.Nodes)
	case *ast.MapNode:
		visitProjectionASTNodes(analyzer, current.Pairs)
	case *ast.PairNode:
		analyzer.visit(current.Key)
		analyzer.visit(current.Value)
	case *ast.NilNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.StringNode, *ast.BytesNode, *ast.ConstantNode, *ast.PointerNode:
		return
	default:
		// Unknown AST node types must use the established full-view behavior.
		analyzer.safe = false
	}
}

func (analyzer *projectionCurrentRootPathAnalyzer) visitIdentifier(node *ast.IdentifierNode) {
	if isProjectionCurrentRootName(node.Value) {
		analyzer.safe = false
		return
	}
	if _, isRootBinding := analyzer.rootBindings[node.Value]; isRootBinding {
		analyzer.safe = false
	}
}

func (analyzer *projectionCurrentRootPathAnalyzer) visitMember(node *ast.MemberNode) {
	if projectionNodeHasCurrentRootBase(node.Node, analyzer.rootBindings) {
		analyzer.safe = false
		return
	}
	analyzer.visit(node.Node)
	analyzer.visit(node.Property)
}

func visitProjectionASTNodes[T ast.Node](analyzer *projectionCurrentRootPathAnalyzer, nodes []T) {
	for _, node := range nodes {
		analyzer.visit(node)
	}
}

func projectionCurrentRootMemberPath(node ast.Node, rootBindings map[string]struct{}) ([]string, bool) {
	switch current := node.(type) {
	case *ast.IdentifierNode:
		if isProjectionCurrentRootName(current.Value) {
			return nil, true
		}
		if _, isRootBinding := rootBindings[current.Value]; isRootBinding {
			return []string{current.Value}, true
		}
	case *ast.ChainNode:
		return projectionCurrentRootMemberPath(current.Node, rootBindings)
	case *ast.MemberNode:
		if current.Method {
			return nil, false
		}
		property, ok := current.Property.(*ast.StringNode)
		if !ok {
			return nil, false
		}
		path, ok := projectionCurrentRootMemberPath(current.Node, rootBindings)
		if !ok {
			return nil, false
		}
		return append(path, property.Value), true
	}
	return nil, false
}

func isProjectionCurrentRootName(name string) bool {
	return name == expressionRootName || name == expressionStateName
}

func projectionNodeHasCurrentRootBase(node ast.Node, rootBindings map[string]struct{}) bool {
	switch current := node.(type) {
	case *ast.IdentifierNode:
		if isProjectionCurrentRootName(current.Value) {
			return true
		}
		_, isRootBinding := rootBindings[current.Value]
		return isRootBinding
	case *ast.ChainNode:
		return projectionNodeHasCurrentRootBase(current.Node, rootBindings)
	case *ast.MemberNode:
		return projectionNodeHasCurrentRootBase(current.Node, rootBindings)
	default:
		return false
	}
}

// expressionRootMapVisibleBindings mirrors the fields made available as bare
// expression bindings by normalizeExpressionEnvironment.
func expressionRootMapVisibleBindings(root map[string]any, protected map[string]struct{}) map[string]struct{} {
	bindings := make(map[string]struct{}, len(root)*2)
	add := func(name string) {
		if name == expressionRootName || name == expressionStateName || name == expressionInputName || projectionFieldIsBuiltin(name) {
			return
		}
		if _, isProtected := protected[name]; isProtected {
			return
		}
		bindings[name] = struct{}{}
	}
	for name := range root {
		add(name)
		if expressionMapKeyHasUppercase(name) {
			alias := snakeCase(name)
			if alias != name {
				add(alias)
			}
		}
	}
	return bindings
}

func expressionRootMapFieldForStaticPath(root map[string]any, selector string) (field string, exists, safe bool) {
	if _, exists := root[selector]; exists {
		return selector, true, true
	}
	for name := range root {
		if name == selector || !expressionMapKeyHasUppercase(name) {
			continue
		}
		if snakeCase(name) == selector {
			// The selector is an expression-view alias rather than an exact map
			// key. Fall back to the full view to preserve collision precedence.
			return "", false, false
		}
	}
	return "", false, true
}

func expressionValueViewWithRootSelection(value any, typeCache *expressionViewTypeCache, selection *expressionRootViewSelection) any {
	root, ok := value.(map[string]any)
	if !ok || root == nil || selection == nil {
		return expressionValueViewWithCache(value, typeCache)
	}
	state := &expressionViewState{typeCache: typeCache}
	rootValue := reflect.ValueOf(root)
	key, tracked := expressionViewReferenceKey(rootValue)
	activeIndex := -1
	if tracked {
		if view, exists := state.activeView(key); exists {
			return view
		}
		activeIndex = state.pushActive(key)
	}
	view := expressionStringAnyMapSelectedView(root, selection, state, activeIndex)
	state.popActive(activeIndex)
	if state.partialReference {
		return expressionValueViewWithCache(value, typeCache)
	}
	return view
}

func expressionStringAnyMapSelectedView(value map[string]any, selection *expressionRootViewSelection, state *expressionViewState, activeIndex int) any {
	if value == nil {
		return nil
	}
	var smallAliases [8]expressionMapAlias
	aliases := smallAliases[:0]
	out := maps.Clone(value)
	state.setActivePartialView(activeIndex, out)
	for name, child := range value {
		if _, selectedField := selection.topLevelFields[name]; selectedField {
			nested := selection.nestedFields[name]
			if nested == nil {
				if !expressionValueViewIsScalar(child) {
					out[name] = expressionValueViewDepth(reflect.ValueOf(child), state)
				}
			} else if childMap, ok := child.(map[string]any); ok {
				out[name] = expressionValueViewSelectedMap(childMap, nested, state)
			} else if !expressionValueViewIsScalar(child) {
				out[name] = expressionValueViewDepth(reflect.ValueOf(child), state)
			}
		}
		if !expressionMapKeyHasUppercase(name) {
			continue
		}
		alias := state.snakeCaseAlias(name)
		if alias != name {
			aliases = append(aliases, expressionMapAlias{name: name, alias: alias})
		}
	}
	if len(aliases) > 1 {
		slices.SortFunc(aliases, func(left, right expressionMapAlias) int {
			return strings.Compare(left.name, right.name)
		})
	}
	for _, item := range aliases {
		if _, exists := out[item.alias]; !exists {
			out[item.alias] = out[item.name]
		}
	}
	return out
}

func expressionValueViewSelectedMap(value map[string]any, selection *expressionRootViewSelection, state *expressionViewState) any {
	if value == nil {
		return nil
	}
	key, tracked := expressionViewReferenceKey(reflect.ValueOf(value))
	activeIndex := -1
	if tracked {
		if view, exists := state.activeView(key); exists {
			return view
		}
		activeIndex = state.pushActive(key)
	}
	view := expressionStringAnyMapSelectedView(value, selection, state, activeIndex)
	state.popActive(activeIndex)
	return view
}

func sameProjectionVisibleNames(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for name := range left {
		if _, exists := right[name]; !exists {
			return false
		}
	}
	return true
}
