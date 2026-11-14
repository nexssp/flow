package projection

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"

	"github.com/nexssp/flow/compiler"
)

type projectionMissingFieldError struct {
	parentType reflect.Type
	selector   string
}

func (err *projectionMissingFieldError) Error() string {
	return fmt.Sprintf("type %s has no field %q", err.parentType, err.selector)
}

func isProjectionSchemaType(valueType reflect.Type) bool {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType == nil || valueType.Kind() != reflect.Struct {
		return false
	}
	for field := range valueType.Fields() {
		if field.Tag.Get("flow_projection") != "true" {
			return false
		}
	}
	return true
}

func projectionExpressionProxyDepth(source string) int {
	tree, err := parser.Parse(source)
	if err != nil {
		return strings.Count(source, ".") + strings.Count(source, "[") + strings.Count(source, "(") + 1
	}
	depth := 1
	ast.Walk(&tree.Node, projectionProxyDepthVisitor{depth: &depth})
	return depth
}

type projectionProxyDepthVisitor struct {
	depth *int
}

//nolint:gocritic // ast.Visitor requires a *ast.Node parameter.
func (visitor projectionProxyDepthVisitor) Visit(node *ast.Node) {
	if depth := projectionASTPathDepth(*node); depth > *visitor.depth {
		*visitor.depth = depth
	}
}

func projectionASTPathDepth(node ast.Node) int {
	switch current := node.(type) {
	case *ast.MemberNode:
		return projectionASTPathDepth(current.Node) + 1
	case *ast.CallNode:
		return projectionASTPathDepth(current.Callee) + 1
	case *ast.ChainNode:
		return projectionASTPathDepth(current.Node)
	default:
		return 0
	}
}

func validateTypedExpressionPaths(source string, pos compiler.Position, staticTypes map[string]reflect.Type, checkBareIdentifiers bool) error {
	tree, err := parser.Parse(source)
	if err != nil {
		return compiler.SourceError(pos, "projection: %v", err)
	}
	localTypes := maps.Clone(staticTypes)
	if localTypes == nil {
		localTypes = make(map[string]reflect.Type)
	}
	if err := collectProjectionLocalTypes(tree.Node, localTypes); err != nil {
		return compiler.SourceError(pos, "projection: %v", err)
	}
	optionalMembers := make(map[ast.Node]struct{})
	optionalIdentifiers := make(map[string]struct{})
	ast.Walk(&tree.Node, projectionNullCoalesceVisitor{members: optionalMembers, identifiers: optionalIdentifiers})
	validator := projectionStaticPathValidator{types: localTypes, optionalMembers: optionalMembers}
	ast.Walk(&tree.Node, &validator)
	if validator.err != nil {
		return compiler.SourceError(pos, "projection: %v", validator.err)
	}
	if checkBareIdentifiers {
		if err := validateBareProjectionIdentifiers(source, localTypes, optionalIdentifiers); err != nil {
			return compiler.SourceError(pos, "projection: %v", err)
		}
	}
	return nil
}

func collectProjectionLocalTypes(node ast.Node, staticTypes map[string]reflect.Type) error {
	switch current := node.(type) {
	case *ast.VariableDeclaratorNode:
		valueType, known, err := projectionASTNodeType(current.Value, staticTypes)
		if err != nil {
			return err
		}
		if !known || valueType == nil {
			valueType = reflect.TypeFor[any]()
		}
		staticTypes[current.Name] = valueType
		return collectProjectionLocalTypes(current.Expr, staticTypes)
	case *ast.SequenceNode:
		for _, child := range current.Nodes {
			if err := collectProjectionLocalTypes(child, staticTypes); err != nil {
				return err
			}
		}
	}
	return nil
}

type projectionStaticPathValidator struct {
	types           map[string]reflect.Type
	optionalMembers map[ast.Node]struct{}
	err             error
}

//nolint:gocritic // ast.Visitor requires a *ast.Node parameter.
func (validator *projectionStaticPathValidator) Visit(node *ast.Node) {
	if validator.err != nil {
		return
	}
	switch current := (*node).(type) {
	case *ast.MemberNode:
		_, _, validator.err = projectionASTNodeType(current, validator.types)
		if validator.err != nil {
			if _, optional := validator.optionalMembers[current]; optional {
				var missingField *projectionMissingFieldError
				if errors.As(validator.err, &missingField) && isProjectionSchemaType(missingField.parentType) {
					validator.err = nil
				}
			}
		}
	case *ast.CallNode, *ast.SliceNode:
		_, _, validator.err = projectionASTNodeType(current, validator.types)
	}
}

type projectionNullCoalesceVisitor struct {
	members     map[ast.Node]struct{}
	identifiers map[string]struct{}
}

//nolint:gocritic // ast.Visitor requires a *ast.Node parameter.
func (visitor projectionNullCoalesceVisitor) Visit(node *ast.Node) {
	if binary, ok := (*node).(*ast.BinaryNode); ok && binary.Operator == "??" {
		addOptionalProjectionPath(binary.Left, visitor.members, visitor.identifiers)
	}
}

func addOptionalProjectionPath(node ast.Node, members map[ast.Node]struct{}, identifiers map[string]struct{}) {
	switch current := node.(type) {
	case *ast.MemberNode:
		members[current] = struct{}{}
	case *ast.IdentifierNode:
		identifiers[current.Value] = struct{}{}
	case *ast.ChainNode:
		addOptionalProjectionPath(current.Node, members, identifiers)
	}
}

func projectionASTNodeType(node ast.Node, staticTypes map[string]reflect.Type) (reflect.Type, bool, error) {
	switch current := node.(type) {
	case *ast.IdentifierNode:
		valueType, exists := staticTypes[current.Value]
		return valueType, exists, nil
	case *ast.MemberNode:
		return projectionASTMemberType(current, staticTypes)
	case *ast.CallNode:
		return projectionASTCallType(current, staticTypes)
	case *ast.ChainNode:
		return projectionASTNodeType(current.Node, staticTypes)
	case *ast.SliceNode:
		return projectionASTSliceType(current, staticTypes)
	case *ast.MapNode, *ast.ArrayNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.StringNode, *ast.NilNode, *ast.ConstantNode:
		return projectionOutputType(node), true, nil
	default:
		return nil, false, nil
	}
}

func projectionASTMemberType(member *ast.MemberNode, staticTypes map[string]reflect.Type) (reflect.Type, bool, error) {
	baseType, known, err := projectionASTNodeType(member.Node, staticTypes)
	if err != nil || !known {
		return nil, false, err
	}
	name, isString := member.Property.(*ast.StringNode)
	if member.Method {
		if !isString {
			return nil, false, nil
		}
		return projectionStaticMethodType(baseType, name.Value)
	}
	if isString {
		return projectionStaticMemberType(baseType, name.Value)
	}
	return projectionStaticIndexType(baseType)
}

func projectionASTCallType(call *ast.CallNode, staticTypes map[string]reflect.Type) (reflect.Type, bool, error) {
	calleeType, known, err := projectionASTNodeType(call.Callee, staticTypes)
	if err != nil || !known || calleeType == nil || calleeType.Kind() != reflect.Func || calleeType.NumOut() == 0 {
		return nil, false, err
	}
	return calleeType.Out(0), true, nil
}

func projectionASTSliceType(slice *ast.SliceNode, staticTypes map[string]reflect.Type) (reflect.Type, bool, error) {
	baseType, known, err := projectionASTNodeType(slice.Node, staticTypes)
	if err != nil || !known {
		return nil, false, err
	}
	for baseType != nil && baseType.Kind() == reflect.Pointer {
		baseType = baseType.Elem()
	}
	if baseType != nil && (baseType.Kind() == reflect.Slice || baseType.Kind() == reflect.Array) {
		return reflect.SliceOf(baseType.Elem()), true, nil
	}
	return nil, false, nil
}

func projectionStaticMemberType(valueType reflect.Type, selector string) (reflect.Type, bool, error) {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType == nil || valueType.Kind() == reflect.Interface {
		return nil, false, nil
	}
	if valueType.Kind() == reflect.Map {
		return valueType.Elem(), true, nil
	}
	if valueType.Kind() != reflect.Struct {
		return nil, false, fmt.Errorf("type %s has no field %q", valueType, selector)
	}
	fieldType, found := projectionStaticFieldType(valueType, selector)
	if found {
		return fieldType, true, nil
	}
	if _, found := valueType.MethodByName(selector); found {
		return nil, true, nil
	}
	return nil, false, &projectionMissingFieldError{parentType: valueType, selector: selector}
}

func projectionStaticMethodType(valueType reflect.Type, selector string) (reflect.Type, bool, error) {
	if fieldType, found := projectionStaticFieldType(valueType, selector); found {
		return fieldType, true, nil
	}
	if valueType == nil || valueType.Kind() == reflect.Interface {
		return nil, false, nil
	}
	method, found := valueType.MethodByName(selector)
	if !found {
		return nil, false, fmt.Errorf("type %s has no method %q", valueType, selector)
	}
	return boundMethodType(method.Type), true, nil
}

func projectionStaticIndexType(valueType reflect.Type) (reflect.Type, bool, error) {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType == nil || valueType.Kind() == reflect.Interface {
		return nil, false, nil
	}
	//nolint:exhaustive // Only indexable aggregate and string kinds need explicit result types.
	switch valueType.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		return valueType.Elem(), true, nil
	case reflect.String:
		return reflect.TypeFor[uint8](), true, nil
	default:
		return nil, false, fmt.Errorf("type %s cannot be indexed", valueType)
	}
}

func validateBareProjectionIdentifiers(source string, staticTypes map[string]reflect.Type, optionalIdentifiers map[string]struct{}) error {
	for index := 0; index < len(source); {
		next, start, name, found := nextProjectionBareIdentifier(source, index)
		index = next
		if !found {
			continue
		}
		if err := validateBareProjectionIdentifier(source, start, next, name, staticTypes, optionalIdentifiers); err != nil {
			return err
		}
	}
	return nil
}

func nextProjectionBareIdentifier(source string, index int) (next, identifierStart int, name string, found bool) {
	if source[index] == '#' {
		for index < len(source) && source[index] != '\n' && source[index] != '\r' {
			index++
		}
		return index, 0, "", false
	}
	if isProjectionQuote(source[index]) {
		return skipProjectionQuoted(source, index), 0, "", false
	}
	if !isProjectionIdentifierStart(source[index]) {
		return index + 1, 0, "", false
	}
	identifierStart = index
	index++
	for index < len(source) && isProjectionIdentifierContinue(source[index]) {
		index++
	}
	return index, identifierStart, source[identifierStart:index], true
}

func validateBareProjectionIdentifier(source string, start, end int, name string, staticTypes map[string]reflect.Type, optionalIdentifiers map[string]struct{}) error {
	next := nextProjectionSignificant(source, end)
	if _, hasStaticType := staticTypes[name]; hasStaticType || next == ':' || next == '(' || next == '.' && isProjectionSymbolNamespace(name) || projectionFieldIsBuiltin(name) {
		return nil
	}
	previous := previousProjectionSignificant(source, start)
	if previous == '.' || previous == ']' || previous == ')' || isProjectionKeyword(name) {
		return nil
	}
	rootType := staticTypes[expressionRootName]
	for rootType != nil && rootType.Kind() == reflect.Pointer {
		rootType = rootType.Elem()
	}
	if _, optional := optionalIdentifiers[name]; optional && isProjectionSchemaType(rootType) {
		return nil
	}
	if rootType != nil && rootType.Kind() == reflect.Struct {
		return fmt.Errorf("type %s has no field %q", rootType, name)
	}
	return nil
}

func isProjectionKeyword(name string) bool {
	switch name {
	case "true", "false", "nil", "not", "in", "and", "or", "matches", "contains", "startsWith", "endsWith", "let", "if", "else":
		return true
	default:
		return false
	}
}

func isProjectionSymbolNamespace(name string) bool {
	for _, namespace := range compiler.Namespaces() {
		if namespace.Name == name {
			return true
		}
	}
	return false
}
