package projection

import (
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/expr-lang/expr/builtin"
)

const (
	expressionRootName  = "__root__"
	expressionStateName = "__state__"
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
	active    map[expressionViewKey]any
	typeCache *expressionViewTypeCache
}

const maxCachedExpressionViewTypes = 128

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
	if tracked {
		if view, exists := state.active[key]; exists {
			return view
		}
		if state.active == nil {
			state.active = make(map[expressionViewKey]any)
		}
		state.active[key] = nil
		defer delete(state.active, key)
	}

	//nolint:exhaustive // Only pointers, structs, maps and sequences need recursive conversion.
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		if value.Elem().Kind() == reflect.Struct {
			return expressionStructView(value.Elem(), value, state, key, tracked)
		}
		return expressionValueViewDepth(value.Elem(), state)
	case reflect.Struct:
		return expressionStructView(value, value, state, key, tracked)
	case reflect.Map:
		return expressionMapView(value, state, key, tracked)
	case reflect.Slice, reflect.Array:
		return expressionSequenceView(value, state, key, tracked)
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return nil
	}
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

func expressionMapView(value reflect.Value, state *expressionViewState, key expressionViewKey, tracked bool) any {
	if value.Type().Key().Kind() != reflect.String {
		return value.Interface()
	}
	if value.IsNil() {
		return value.Interface()
	}
	out := make(map[string]any, value.Len()*2)
	keys := make([]string, 0, value.Len())
	iterator := value.MapRange()
	for iterator.Next() {
		keys = append(keys, iterator.Key().String())
	}
	sort.Strings(keys)
	if tracked {
		state.active[key] = out
	}
	for _, name := range keys {
		mapKey := reflect.ValueOf(name)
		if mapKey.Type() != value.Type().Key() {
			mapKey = mapKey.Convert(value.Type().Key())
		}
		out[name] = expressionValueViewDepth(value.MapIndex(mapKey), state)
	}
	for _, name := range keys {
		alias := snakeCase(name)
		if alias != name {
			if _, exists := out[alias]; !exists {
				out[alias] = out[name]
			}
		}
	}
	return out
}

func expressionSequenceView(value reflect.Value, state *expressionViewState, key expressionViewKey, tracked bool) any {
	if value.Kind() == reflect.Slice && value.IsNil() {
		return []any(nil)
	}
	out := make([]any, value.Len())
	if tracked {
		state.active[key] = out
	}
	for index := range value.Len() {
		out[index] = expressionValueViewDepth(value.Index(index), state)
	}
	return out
}

func expressionStructView(value, methodReceiver reflect.Value, state *expressionViewState, key expressionViewKey, tracked bool) any {
	fields := state.typeCache.fieldsFor(value.Type())
	if len(fields) == 0 && methodReceiver.NumMethod() == 0 {
		return value.Interface()
	}
	out := make(map[string]any, len(fields)*2+methodReceiver.NumMethod())
	if tracked {
		state.active[key] = out
	}
	type projectedField struct {
		value any
		valid bool
	}
	var small [8]projectedField
	projected := small[:len(fields)]
	if len(fields) > len(small) {
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

func normalizeExpressionEnvironmentWithCache(environment map[string]any, typeCache *expressionViewTypeCache) map[string]any {
	out := make(map[string]any, len(environment)+2)
	root, hasInputRoot := environment[expressionRootName].(expressionEnvironmentRoot)
	for name, value := range environment {
		if name == expressionRootName && hasInputRoot {
			out[name] = expressionValueViewWithCache(root.value, typeCache)
			continue
		}
		out[name] = expressionValueViewWithCache(value, typeCache)
	}
	if hasInputRoot {
		rootView := out[expressionRootName]
		if fields, ok := rootView.(map[string]any); ok {
			for name, value := range fields {
				if name != expressionRootName && name != expressionStateName && !projectionFieldIsBuiltin(name) {
					out[name] = value
				}
			}
		}
		out[expressionRootName] = rootView
		out[expressionStateName] = rootView
	}
	return out
}
