package projection

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/expr-lang/expr/builtin"

	"github.com/nexssp/flow/compiler"
)

const (
	expressionRootName          = "__root__"
	expressionStateName         = "__state__"
	expressionInputName         = "input"
	expressionInputViewCacheKey = "flow.projection.expression-value-view.v1"
)

func projectionFieldIsBuiltin(name string) bool {
	_, exists := builtin.Index[name]
	return exists
}

type expressionViewField struct {
	Name            string
	SnakeName       string
	ExpressionName  string
	ExpressionSnake string
	JSONName        string
	Type            reflect.Type
	Tag             reflect.StructTag
	Index           []int
}

type expressionViewKey struct {
	typeOf   reflect.Type
	pointer  uintptr
	length   int
	capacity int
}

type expressionViewState struct {
	active           []expressionViewEntry
	smallActive      [8]expressionViewEntry
	typeCache        *expressionViewTypeCache
	snakeCaseAliases map[string]string
	partialReference bool
}

type expressionViewEntry struct {
	key     expressionViewKey
	view    any
	partial bool
}

const (
	maxCachedExpressionViewTypes   = 128
	maxCachedExpressionViewAliases = 256
)

type expressionViewTypeCache struct {
	mu     sync.RWMutex
	fields map[reflect.Type][]expressionViewField
}

func newExpressionViewTypeCache() *expressionViewTypeCache {
	return &expressionViewTypeCache{}
}

func (cache *expressionViewTypeCache) fieldsFor(valueType reflect.Type) []expressionViewField {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType == nil || valueType.Kind() != reflect.Struct {
		return nil
	}
	if cache == nil {
		return expressionViewFields(valueType)
	}
	cache.mu.RLock()
	fields, exists := cache.fields[valueType]
	cache.mu.RUnlock()
	if exists {
		return fields
	}
	fields = expressionViewFields(valueType)
	cache.mu.Lock()
	if cached, found := cache.fields[valueType]; found {
		fields = cached
	} else if len(cache.fields) < maxCachedExpressionViewTypes {
		if cache.fields == nil {
			cache.fields = make(map[reflect.Type][]expressionViewField, 8)
		}
		cache.fields[valueType] = fields
	}
	cache.mu.Unlock()
	return fields
}

// expressionViewFields returns exported fields in Go's normal direct-field
// precedence, followed by promoted fields from embedded structs.
func expressionViewFields(valueType reflect.Type) []expressionViewField {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType.Kind() != reflect.Struct {
		return nil
	}
	type fieldSource struct {
		typeOf reflect.Type
		prefix []int
		path   map[reflect.Type]bool
	}
	var fields []expressionViewField
	seenNames := make(map[string]bool)
	layer := []fieldSource{{typeOf: valueType, path: make(map[reflect.Type]bool)}}
	for len(layer) > 0 {
		candidates := make(map[string][]expressionViewField)
		var next []fieldSource
		for _, source := range layer {
			if source.path[source.typeOf] {
				continue
			}
			path := make(map[reflect.Type]bool, len(source.path)+1)
			for seen := range source.path {
				path[seen] = true
			}
			path[source.typeOf] = true
			for field := range source.typeOf.Fields() {
				index := append(append([]int(nil), source.prefix...), field.Index...)
				if field.IsExported() {
					expressionName := field.Tag.Get("expr")
					jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
					if jsonName == "-" {
						jsonName = ""
					}
					candidates[field.Name] = append(candidates[field.Name], expressionViewField{
						Name: field.Name, SnakeName: snakeCase(field.Name),
						ExpressionName: expressionName, ExpressionSnake: snakeCase(expressionName), JSONName: jsonName,
						Type: field.Type, Tag: field.Tag, Index: index,
					})
				}
				if !field.Anonymous {
					continue
				}
				embedded := field.Type
				for embedded.Kind() == reflect.Pointer {
					embedded = embedded.Elem()
				}
				if embedded.Kind() == reflect.Struct {
					next = append(next, fieldSource{typeOf: embedded, prefix: index, path: path})
				}
			}
		}
		for _, source := range layer {
			for field := range source.typeOf.Fields() {
				name := field.Name
				matches := candidates[name]
				if seenNames[name] || len(matches) == 0 {
					continue
				}
				seenNames[name] = true
				if len(matches) == 1 {
					fields = append(fields, matches[0])
				}
			}
		}
		layer = next
	}
	return fields
}

func expressionValueView(value any) any {
	return expressionValueViewWithCache(value, nil)
}

func expressionValueViewWithCache(value any, typeCache *expressionViewTypeCache) any {
	state := &expressionViewState{typeCache: typeCache}
	return expressionValueViewDepth(reflect.ValueOf(value), state)
}

func expressionValueViewDepth(value reflect.Value, state *expressionViewState) any {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	key, tracked := expressionViewReferenceKey(value)
	activeIndex := -1
	if tracked {
		if view, exists := state.activeView(key); exists {
			return view
		}
		activeIndex = state.pushActive(key)
	}

	var view any
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			if value.Elem().Kind() == reflect.Struct {
				view = expressionStructView(value.Elem(), value, state, activeIndex)
			} else {
				view = expressionValueViewDepth(value.Elem(), state)
			}
		}
	case reflect.Struct:
		view = expressionStructView(value, value, state, activeIndex)
	case reflect.Map:
		view = expressionMapView(value, state, activeIndex)
	case reflect.Slice, reflect.Array:
		view = expressionSequenceView(value, state, activeIndex)
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.Interface, reflect.String, reflect.UnsafePointer:
		if value.CanInterface() {
			view = value.Interface()
		}
	}
	state.popActive(activeIndex)
	return view
}

func (state *expressionViewState) activeView(key expressionViewKey) (any, bool) {
	for _, entry := range slices.Backward(state.active) {
		if entry.key == key {
			if entry.partial {
				state.partialReference = true
			}
			return entry.view, true
		}
	}
	return nil, false
}

func (state *expressionViewState) pushActive(key expressionViewKey) int {
	if state.active == nil {
		state.active = state.smallActive[:0]
	}
	index := len(state.active)
	state.active = append(state.active, expressionViewEntry{key: key})
	return index
}

func (state *expressionViewState) setActiveView(index int, view any) {
	if index >= 0 {
		state.active[index].view = view
	}
}

func (state *expressionViewState) setActivePartialView(index int, view any) {
	if index >= 0 {
		state.active[index].view = view
		state.active[index].partial = true
	}
}

func (state *expressionViewState) popActive(index int) {
	if index < 0 {
		return
	}
	state.active[index] = expressionViewEntry{}
	state.active = state.active[:index]
}

func expressionViewReferenceKey(value reflect.Value) (expressionViewKey, bool) {
	//nolint:exhaustive // Only pointer-like kinds need active-cycle identities.
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return expressionViewKey{}, false
		}
		return expressionViewKey{typeOf: value.Type(), pointer: value.Pointer()}, true
	case reflect.Map:
		if value.IsNil() {
			return expressionViewKey{}, false
		}
		return expressionViewKey{typeOf: value.Type(), pointer: value.Pointer()}, true
	case reflect.Slice:
		if value.IsNil() || value.Len() == 0 {
			return expressionViewKey{}, false
		}
		return expressionViewKey{
			typeOf: value.Type(), pointer: value.Pointer(), length: value.Len(), capacity: value.Cap(),
		}, true
	default:
		return expressionViewKey{}, false
	}
}

func expressionMapView(value reflect.Value, state *expressionViewState, activeIndex int) any {
	if value.Type().Key().Kind() != reflect.String {
		return value.Interface()
	}
	if value.IsNil() {
		return value.Interface()
	}
	if value.Type() == reflect.TypeFor[map[string]any]() {
		items, ok := reflect.TypeAssert[map[string]any](value)
		if !ok {
			return value.Interface()
		}
		return expressionStringAnyMapView(items, state, activeIndex)
	}
	out := make(map[string]any, value.Len()*2)
	keys := make([]string, 0, value.Len())
	iterator := value.MapRange()
	for iterator.Next() {
		keys = append(keys, iterator.Key().String())
	}
	sort.Strings(keys)
	state.setActiveView(activeIndex, out)
	for _, name := range keys {
		mapKey := reflect.ValueOf(name)
		if mapKey.Type() != value.Type().Key() {
			mapKey = mapKey.Convert(value.Type().Key())
		}
		out[name] = expressionValueViewDepth(value.MapIndex(mapKey), state)
	}
	for _, name := range keys {
		if !expressionMapKeyHasUppercase(name) {
			continue
		}
		alias := state.snakeCaseAlias(name)
		if alias != name {
			if _, exists := out[alias]; !exists {
				out[alias] = out[name]
			}
		}
	}
	return out
}

type expressionMapAlias struct {
	name  string
	alias string
}

func expressionStringAnyMapView(value map[string]any, state *expressionViewState, activeIndex int) any {
	if value == nil {
		return nil
	}
	var smallAliases [8]expressionMapAlias
	aliases := smallAliases[:0]
	out := maps.Clone(value)
	state.setActiveView(activeIndex, out)
	for name, child := range value {
		if !expressionValueViewIsScalar(child) {
			out[name] = expressionValueViewAny(child, state)
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

func expressionMapKeyHasUppercase(name string) bool {
	for index := range len(name) {
		current := name[index]
		if current >= 'A' && current <= 'Z' {
			return true
		}
		if current >= 0x80 {
			for _, current := range name[index:] {
				if unicode.IsUpper(current) {
					return true
				}
			}
			return false
		}
	}
	return false
}

func expressionValueViewAny(value any, state *expressionViewState) any {
	if expressionValueViewIsScalar(value) {
		return value
	}
	return expressionValueViewDepth(reflect.ValueOf(value), state)
}

func expressionValueViewIsScalar(value any) bool {
	switch value.(type) {
	case nil, bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	default:
		return false
	}
}

func (state *expressionViewState) snakeCaseAlias(name string) string {
	if alias, exists := state.snakeCaseAliases[name]; exists {
		return alias
	}
	alias := snakeCase(name)
	if alias == name || len(state.snakeCaseAliases) >= maxCachedExpressionViewAliases {
		return alias
	}
	if state.snakeCaseAliases == nil {
		state.snakeCaseAliases = make(map[string]string, 16)
	}
	state.snakeCaseAliases[name] = alias
	return alias
}

func expressionSequenceView(value reflect.Value, state *expressionViewState, activeIndex int) any {
	if value.Type() == reflect.TypeFor[[]any]() {
		items, ok := reflect.TypeAssert[[]any](value)
		if !ok {
			return value.Interface()
		}
		if items == nil {
			return []any(nil)
		}
		out := make([]any, len(items))
		state.setActiveView(activeIndex, out)
		for index, item := range items {
			out[index] = expressionValueViewAny(item, state)
		}
		return out
	}
	if value.Kind() == reflect.Slice && expressionScalarKind(value.Type().Elem().Kind()) {
		if value.IsNil() {
			return reflect.Zero(value.Type()).Interface()
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		reflect.Copy(out, value)
		view := out.Interface()
		state.setActiveView(activeIndex, view)
		return view
	}
	if value.Kind() == reflect.Slice && value.IsNil() {
		return []any(nil)
	}
	out := make([]any, value.Len())
	state.setActiveView(activeIndex, out)
	for index := range value.Len() {
		out[index] = expressionValueViewDepth(value.Index(index), state)
	}
	return out
}

func expressionScalarKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.String:
		return true
	case reflect.Invalid, reflect.Uintptr, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice, reflect.Struct, reflect.UnsafePointer:
		return false
	}
	return false
}

func expressionStructView(value, methodReceiver reflect.Value, state *expressionViewState, activeIndex int) any {
	fields := state.typeCache.fieldsFor(value.Type())
	if len(fields) == 0 && methodReceiver.NumMethod() == 0 {
		return value.Interface()
	}
	out := make(map[string]any, len(fields)*2+methodReceiver.NumMethod())
	state.setActiveView(activeIndex, out)
	type projectedField struct {
		value any
		valid bool
	}
	var small [8]projectedField
	var projected []projectedField
	if len(fields) <= len(small) {
		projected = small[:len(fields)]
	} else {
		projected = make([]projectedField, len(fields))
	}
	for index := range fields {
		field := &fields[index]
		fieldValue, ok := expressionFieldValue(value, field.Index)
		if !ok || !fieldValue.IsValid() || !fieldValue.CanInterface() {
			continue
		}
		projected[index] = projectedField{value: expressionValueViewDepth(fieldValue, state), valid: true}
	}
	for index := range fields {
		field := &fields[index]
		current := projected[index]
		if !current.valid {
			continue
		}
		setExpressionAlias(out, field.Name, current.value)
		if field.ExpressionName != "" {
			setExpressionAlias(out, field.ExpressionName, current.value)
		}
		if field.JSONName != "" {
			setExpressionAlias(out, field.JSONName, current.value)
		}
	}
	for index := range fields {
		field := &fields[index]
		current := projected[index]
		if !current.valid {
			continue
		}
		setExpressionAlias(out, field.SnakeName, current.value)
		if field.ExpressionName != "" {
			setExpressionAlias(out, field.ExpressionSnake, current.value)
		}
	}
	for i := range methodReceiver.NumMethod() {
		method := methodReceiver.Type().Method(i)
		if _, exists := out[method.Name]; exists {
			continue
		}
		bound := methodReceiver.Method(i)
		if bound.IsValid() && bound.CanInterface() {
			out[method.Name] = bound.Interface()
		}
	}
	return out
}

func expressionFieldValue(root reflect.Value, index []int) (reflect.Value, bool) {
	value := root
	for _, part := range index {
		for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
			if value.IsNil() {
				return reflect.Value{}, false
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		value = value.Field(part)
	}
	return value, true
}

func setExpressionAlias(out map[string]any, name string, value any) {
	if name == "" {
		return
	}
	if _, exists := out[name]; !exists {
		out[name] = value
	}
}

func snakeCase(name string) string {
	hasUpper := false
	for index := range len(name) {
		current := name[index]
		if current >= 0x80 {
			return snakeCaseUnicode(name)
		}
		if current >= 'A' && current <= 'Z' {
			hasUpper = true
		}
	}
	if !hasUpper {
		return name
	}
	return snakeCaseASCII(name)
}

func snakeCaseASCII(name string) string {
	var out strings.Builder
	out.Grow(len(name))
	for index := range len(name) {
		current := name[index]
		if current >= 'A' && current <= 'Z' {
			if index > 0 {
				previous := name[index-1]
				nextIsLowerOrDigit := index+1 < len(name) && (isASCIILower(name[index+1]) || isASCIIDigit(name[index+1]))
				if isASCIILower(previous) || isASCIIDigit(previous) ||
					(previous >= 'A' && previous <= 'Z' && nextIsLowerOrDigit) {
					out.WriteByte('_')
				}
			}
			current += 'a' - 'A'
		}
		out.WriteByte(current)
	}
	return out.String()
}

func isASCIILower(value byte) bool {
	return value >= 'a' && value <= 'z'
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func snakeCaseUnicode(name string) string {
	hasUpper := false
	for _, current := range name {
		if unicode.IsUpper(current) {
			hasUpper = true
			break
		}
	}
	if !hasUpper {
		return name
	}
	runes := []rune(name)
	var out strings.Builder
	for index, current := range runes {
		if unicode.IsUpper(current) && index > 0 {
			previous := runes[index-1]
			nextIsLowerOrDigit := index+1 < len(runes) && (unicode.IsLower(runes[index+1]) || unicode.IsDigit(runes[index+1]))
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && nextIsLowerOrDigit) {
				out.WriteByte('_')
			}
		}
		out.WriteRune(unicode.ToLower(current))
	}
	return out.String()
}

type expressionEnvironmentRoot struct {
	value any
}

func normalizeExpressionEnvironmentWithCache(environment map[string]any, typeCache *expressionViewTypeCache, protectedBindings ...map[string]struct{}) map[string]any {
	return normalizeExpressionEnvironmentWithContext(context.TODO(), environment, typeCache, protectedBindings...)
}

func normalizeExpressionEnvironmentWithContext(ctx context.Context, environment map[string]any, typeCache *expressionViewTypeCache, protectedBindings ...map[string]struct{}) map[string]any {
	return normalizeExpressionEnvironment(ctx, environment, typeCache, true, protectedBindings...)
}

// normalizeExpressionEnvironmentForProjection avoids recursively materializing
// the original request when a projection only uses its current payload root.
func normalizeExpressionEnvironmentForProjection(ctx context.Context, environment map[string]any, typeCache *expressionViewTypeCache, source string, protectedBindings ...map[string]struct{}) map[string]any {
	return normalizeExpressionEnvironment(ctx, environment, typeCache, projectionSourceUsesOriginalInput(source), protectedBindings...)
}

func normalizeExpressionEnvironmentForProjectionWithRootSelection(ctx context.Context, environment map[string]any, typeCache *expressionViewTypeCache, source string, selection *expressionRootViewSelection, protectedBindings ...map[string]struct{}) map[string]any {
	return normalizeExpressionEnvironmentWithRootSelection(ctx, environment, typeCache, projectionSourceUsesOriginalInput(source), selection, protectedBindings...)
}

func normalizeExpressionEnvironment(ctx context.Context, environment map[string]any, typeCache *expressionViewTypeCache, includeOriginalInput bool, protectedBindings ...map[string]struct{}) map[string]any {
	return normalizeExpressionEnvironmentWithRootSelection(ctx, environment, typeCache, includeOriginalInput, nil, protectedBindings...)
}

func expressionRootViewForNormalization(value any, typeCache *expressionViewTypeCache, selection *expressionRootViewSelection) any {
	if selection != nil {
		return expressionValueViewWithRootSelection(value, typeCache, selection)
	}
	return expressionValueViewWithCache(value, typeCache)
}

func normalizeExpressionEnvironmentWithRootSelection(ctx context.Context, environment map[string]any, typeCache *expressionViewTypeCache, includeOriginalInput bool, rootSelection *expressionRootViewSelection, protectedBindings ...map[string]struct{}) map[string]any {
	out := make(map[string]any, len(environment)+2)
	var inputView any
	hasInputView := false
	if includeOriginalInput && ctx != nil {
		if _, hasInputBinding := environment[expressionInputName]; hasInputBinding {
			inputView, hasInputView = compiler.CachedExpressionInputView(ctx, expressionInputViewCacheKey, func(input any) any {
				return expressionValueViewWithCache(input, typeCache)
			})
		}
	}
	var protected map[string]struct{}
	if len(protectedBindings) > 0 {
		protected = protectedBindings[0]
	}
	root, hasInputRoot := environment[expressionRootName].(expressionEnvironmentRoot)
	for name, value := range environment {
		if name == expressionInputName && !includeOriginalInput {
			continue
		}
		if name == expressionRootName && hasInputRoot {
			out[name] = expressionRootViewForNormalization(root.value, typeCache, rootSelection)
			continue
		}
		if name == expressionInputName && hasInputView {
			out[name] = inputView
			continue
		}
		out[name] = expressionValueViewWithCache(value, typeCache)
	}
	if hasInputRoot {
		rootView := out[expressionRootName]
		if fields, ok := rootView.(map[string]any); ok {
			for name, value := range fields {
				if name != expressionRootName && name != expressionStateName && name != expressionInputName && !projectionFieldIsBuiltin(name) {
					if _, isProtected := protected[name]; !isProtected {
						out[name] = value
					}
				}
			}
		}
		out[expressionRootName] = rootView
		out[expressionStateName] = rootView
	}
	return out
}

// projectionSourceUsesOriginalInput uses a conservative token scan: string
// literals can cause an unnecessary view, but a bare `input` cannot be missed.
func projectionSourceUsesOriginalInput(source string) bool {
	for offset := 0; offset < len(source); {
		match := strings.Index(source[offset:], expressionInputName)
		if match < 0 {
			return false
		}
		start := offset + match
		end := start + len(expressionInputName)
		if (start == 0 || !expressionIdentifierByte(source[start-1])) &&
			(end == len(source) || !expressionIdentifierByte(source[end])) {
			return true
		}
		offset = end
	}
	return false
}

func expressionIdentifierByte(char byte) bool {
	return char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
}
