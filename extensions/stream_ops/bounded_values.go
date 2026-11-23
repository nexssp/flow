package stream_ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxStreamValueDepth = 64
	mapHeaderAllowance  = 256
	mapEntryAllowance   = 256
	valueCopyAllowance  = 64
)

var (
	errStreamBudgetExceeded    = errors.New("accounted value size exceeds the remaining byte budget")
	errUnsupportedStreamValue  = errors.New("unsupported value for a bounded stream buffer")
	errStreamItemLimitExceeded = errors.New("configured item limit exceeded")
)

type streamValueIdentity struct {
	kind   reflect.Kind
	typeOf reflect.Type
	ptr    uintptr
}

type streamValueMeasure struct {
	limit     uint64
	logical   uint64
	jsonUpper uint64
	stack     map[streamValueIdentity]struct{}
}

type jsonByteCounter struct {
	written uint64
}

func (w *jsonByteCounter) Write(data []byte) (int, error) {
	w.written += uint64(len(data))
	return len(data), nil
}

func checkedStreamLength(length int) (uint64, bool) {
	if length < 0 {
		return 0, false
	}
	return uint64(length), true
}

// prepareStreamItem measures an item before copying it, then returns a detached
// copy. The copy prevents a buffered stream from retaining a larger backing
// array, map table, or string allocation than the item's visible contents.
func prepareStreamItem(item any, remaining uint64) (detached any, accounted uint64, resultErr error) {
	measure := streamValueMeasure{limit: remaining}
	// The outer any value occupies a slot in the operator's []any buffer.
	// A second slot is slack for the buffer's geometric capacity growth.
	if err := measure.addLogical(2 * uint64(reflect.TypeFor[any]().Size())); err != nil {
		return nil, 0, err
	}
	if item == nil {
		if err := measure.addJSON(4); err != nil { // null
			return nil, 0, err
		}
	} else if err := measure.value(reflect.ValueOf(item), true, 0); err != nil {
		return nil, 0, err
	}

	copyValue, err := detachStreamValue(reflect.ValueOf(item), 0)
	if err != nil {
		return nil, 0, err
	}
	if copyValue.IsValid() {
		detached = copyValue.Interface()
	}
	counter := new(jsonByteCounter)
	if err := json.NewEncoder(counter).Encode(detached); err != nil {
		return nil, 0, fmt.Errorf("%w: stream item cannot be encoded as JSON: %w", errUnsupportedStreamValue, err)
	}
	if counter.written == 0 {
		return nil, 0, errors.New("stream item encoder produced no output")
	}
	// Encoder.Encode appends one newline; account only for the JSON value itself.
	encodedSize := counter.written - 1
	if encodedSize > measure.jsonUpper {
		return nil, 0, errors.New("stream item exceeded its checked JSON size bound")
	}
	accounted = measure.logical + measure.jsonUpper
	return detached, accounted, nil
}

func (m *streamValueMeasure) addLogical(n uint64) error {
	if n > m.limit-m.logical || m.jsonUpper > m.limit-m.logical-n {
		return errStreamBudgetExceeded
	}
	m.logical += n
	return nil
}

func (m *streamValueMeasure) addJSON(n uint64) error {
	if n > m.limit-m.jsonUpper || m.logical > m.limit-m.jsonUpper-n {
		return errStreamBudgetExceeded
	}
	m.jsonUpper += n
	return nil
}

func (m *streamValueMeasure) addLogicalProduct(count, size uint64) error {
	if count == 0 || size == 0 {
		return nil
	}
	remaining := m.limit - m.logical - m.jsonUpper
	if count > remaining/size {
		return errStreamBudgetExceeded
	}
	m.logical += count * size
	return nil
}

func (m *streamValueMeasure) value(value reflect.Value, includeInline bool, depth int) error {
	if depth > maxStreamValueDepth {
		return fmt.Errorf("%w: nesting exceeds %d levels", errUnsupportedStreamValue, maxStreamValueDepth)
	}
	if !value.IsValid() {
		return m.addJSON(4)
	}
	typeOf := value.Type()
	if hasCustomJSONEncoding(typeOf) {
		return fmt.Errorf("%w: custom JSON encoding on %s", errUnsupportedStreamValue, typeOf)
	}
	if includeInline {
		if err := m.addLogical(uint64(typeOf.Size())); err != nil {
			return err
		}
	}

	switch value.Kind() {
	case reflect.Interface:
		return m.measureInterface(value, depth)
	case reflect.Pointer:
		return m.measurePointer(value, depth)
	case reflect.Bool:
		if value.Bool() {
			return m.addJSON(4) // true
		}
		return m.addJSON(5) // false
	case reflect.String:
		return m.measureString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return m.addJSON(uint64(len(strconv.FormatInt(value.Int(), 10))))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return m.addJSON(uint64(len(strconv.FormatUint(value.Uint(), 10))))
	case reflect.Float32, reflect.Float64:
		return m.measureFloat(value, typeOf)
	case reflect.Slice:
		return m.measureSlice(value, depth)
	case reflect.Array:
		return m.measureArray(value, includeInline, depth)
	case reflect.Map:
		return m.measureMap(value, depth)
	case reflect.Struct:
		return m.measureStruct(value, includeInline, depth)
	case reflect.Invalid, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return fmt.Errorf("%w: %s", errUnsupportedStreamValue, typeOf)
	default:
		return fmt.Errorf("%w: %s", errUnsupportedStreamValue, typeOf)
	}
}

func (m *streamValueMeasure) measureInterface(value reflect.Value, depth int) error {
	if value.IsNil() {
		return m.addJSON(4)
	}
	return m.value(value.Elem(), true, depth+1)
}

func (m *streamValueMeasure) measurePointer(value reflect.Value, depth int) error {
	if value.IsNil() {
		return m.addJSON(4)
	}
	leave, err := m.enter(value)
	if err != nil {
		return err
	}
	defer leave()
	if err := m.addLogical(valueCopyAllowance); err != nil {
		return err
	}
	return m.value(value.Elem(), true, depth+1)
}

func (m *streamValueMeasure) measureString(value reflect.Value) error {
	text := value.String()
	if text != "" {
		if err := m.addLogical(valueCopyAllowance + uint64(len(text))); err != nil {
			return err
		}
	}
	encodedSize, err := jsonStringUpperBound(text, m.limit-m.logical-m.jsonUpper)
	if err != nil {
		return err
	}
	return m.addJSON(encodedSize)
}

func (m *streamValueMeasure) measureFloat(value reflect.Value, typeOf reflect.Type) error {
	f := value.Float()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("%w: non-finite number %s", errUnsupportedStreamValue, typeOf)
	}
	// JSON's shortest float spelling is at most 25 bytes in fixed notation
	// and shorter in exponent notation; 32 leaves room for sign and format
	// normalization, including float32 values rounded across the threshold.
	return m.addJSON(32)
}

func (m *streamValueMeasure) measureSlice(value reflect.Value, depth int) error {
	if value.IsNil() {
		return m.addJSON(4)
	}
	length := value.Len()
	if length < 0 {
		return fmt.Errorf("%w: negative slice length", errUnsupportedStreamValue)
	}
	typeOf := value.Type()
	if typeOf.Elem().Kind() == reflect.Uint8 && !hasCustomJSONEncoding(typeOf.Elem()) {
		if err := m.addLogical(valueCopyAllowance + uint64(length)); err != nil {
			return err
		}
		remaining := m.limit - m.logical - m.jsonUpper
		byteCount := uint64(length)
		if byteCount > remaining {
			return errStreamBudgetExceeded
		}
		encodedSize := uint64(2) + ((byteCount+2)/3)*4
		return m.addJSON(encodedSize)
	}
	leave, err := m.enter(value)
	if err != nil {
		return err
	}
	defer leave()
	if err := m.addLogical(valueCopyAllowance); err != nil {
		return err
	}
	if err := m.addLogicalProduct(uint64(length), uint64(typeOf.Elem().Size())); err != nil {
		return err
	}
	if err := m.addJSON(2); err != nil { // []
		return err
	}
	for index := range value.Len() {
		if index > 0 {
			if err := m.addJSON(1); err != nil {
				return err
			}
		}
		if err := m.value(value.Index(index), false, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (m *streamValueMeasure) measureArray(value reflect.Value, includeInline bool, depth int) error {
	if includeInline {
		if err := m.addLogical(valueCopyAllowance); err != nil {
			return err
		}
	}
	if err := m.addJSON(2); err != nil { // []
		return err
	}
	for index := range value.Len() {
		if index > 0 {
			if err := m.addJSON(1); err != nil {
				return err
			}
		}
		if err := m.value(value.Index(index), false, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (m *streamValueMeasure) measureMap(value reflect.Value, depth int) error {
	if value.IsNil() {
		return m.addJSON(4)
	}
	typeOf := value.Type()
	if typeOf.Key().Kind() != reflect.String || hasCustomJSONEncoding(typeOf.Key()) {
		return fmt.Errorf("%w: map keys must be plain strings, got %s", errUnsupportedStreamValue, typeOf.Key())
	}
	leave, err := m.enter(value)
	if err != nil {
		return err
	}
	defer leave()
	if err := m.addLogical(mapHeaderAllowance); err != nil {
		return err
	}
	entrySize := uint64(mapEntryAllowance) + uint64(typeOf.Key().Size()) + uint64(typeOf.Elem().Size())
	mapLength, ok := checkedStreamLength(value.Len())
	if !ok {
		return errors.New("reflect reported a negative stream map length")
	}
	if err := m.addLogicalProduct(mapLength, entrySize); err != nil {
		return err
	}
	if err := m.addJSON(2); err != nil { // {}
		return err
	}
	iter := value.MapRange()
	first := true
	for iter.Next() {
		key := iter.Key()
		keyLength, ok := checkedStreamLength(key.Len())
		if !ok {
			return errors.New("reflect reported a negative stream map key length")
		}
		if keyLength > 0 {
			if err := m.addLogical(valueCopyAllowance + keyLength); err != nil {
				return err
			}
		}
		if !first {
			if err := m.addJSON(1); err != nil {
				return err
			}
		}
		first = false
		keySize, err := jsonStringUpperBound(key.String(), m.limit-m.logical-m.jsonUpper)
		if err != nil {
			return err
		}
		if err := m.addJSON(keySize + 1); err != nil { // key and colon
			return err
		}
		if err := m.value(iter.Value(), false, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (m *streamValueMeasure) measureStruct(value reflect.Value, includeInline bool, depth int) error {
	typeOf := value.Type()
	if hasUnexportedDynamicFields(typeOf, make(map[reflect.Type]bool)) {
		return fmt.Errorf("%w: %s has unexported dynamic fields", errUnsupportedStreamValue, typeOf)
	}
	if includeInline {
		if err := m.addLogical(valueCopyAllowance); err != nil {
			return err
		}
	}
	if err := m.addJSON(2); err != nil { // {}
		return err
	}
	// Exported fields tagged json:"-" are still measured: the copied struct
	// remains part of the stream value even when JSON omits that field.
	for index := range typeOf.NumField() {
		if err := m.measureStructField(value, typeOf, index, depth); err != nil {
			return err
		}
	}
	return nil
}

func (m *streamValueMeasure) measureStructField(value reflect.Value, typeOf reflect.Type, index, depth int) error {
	fieldType := typeOf.Field(index)
	if fieldType.PkgPath != "" { // unexported
		return nil
	}
	name, options, _ := strings.Cut(fieldType.Tag.Get("json"), ",")
	if name == "" || name == "-" {
		name = fieldType.Name
	}
	if hasOption(options, "string") {
		return fmt.Errorf("%w: ,string field option on %s.%s", errUnsupportedStreamValue, typeOf, fieldType.Name)
	}
	fieldValue := value.Field(index)
	// Even when omitted from JSON, the value is detached and may allocate
	// dynamic storage (notably a non-nil empty map).
	if !hasOption(options, "omitempty") || !isJSONEmpty(fieldValue) {
		if err := m.addJSON(1); err != nil { // comma allowance
			return err
		}
		fieldNameSize, err := jsonStringUpperBound(name, m.limit-m.logical-m.jsonUpper)
		if err != nil {
			return err
		}
		fieldDefaultSize, err := jsonStringUpperBound(fieldType.Name, m.limit-m.logical-m.jsonUpper)
		if err != nil {
			return err
		}
		if fieldDefaultSize > fieldNameSize {
			fieldNameSize = fieldDefaultSize
		}
		if err := m.addJSON(fieldNameSize + 1); err != nil { // name and colon
			return err
		}
	}
	return m.value(fieldValue, false, depth+1)
}

func (m *streamValueMeasure) enter(value reflect.Value) (func(), error) {
	if m.stack == nil {
		m.stack = make(map[streamValueIdentity]struct{})
	}
	identity := streamValueIdentity{kind: value.Kind(), typeOf: value.Type(), ptr: value.Pointer()}
	if _, ok := m.stack[identity]; ok {
		return nil, fmt.Errorf("%w: cyclic value graph", errUnsupportedStreamValue)
	}
	m.stack[identity] = struct{}{}
	return func() { delete(m.stack, identity) }, nil
}

func jsonStringUpperBound(value string, remaining uint64) (uint64, error) {
	size := uint64(2) // quotes
	for index := 0; index < len(value); {
		increment, nextIndex := jsonStringByteUpperBound(value, index)
		if size > remaining || increment > remaining-size {
			return 0, errStreamBudgetExceeded
		}
		size += increment
		index = nextIndex
	}
	if size > remaining {
		return 0, errStreamBudgetExceeded
	}
	return size, nil
}

func jsonStringByteUpperBound(value string, index int) (byteUpperBound uint64, nextIndex int) {
	byteValue := value[index]
	if byteValue < utf8.RuneSelf {
		switch {
		case byteValue == '"' || byteValue == '\\' || byteValue == '\b' || byteValue == '\f' || byteValue == '\n' || byteValue == '\r' || byteValue == '\t':
			return 2, index + 1
		case byteValue < 0x20 || byteValue == '<' || byteValue == '>' || byteValue == '&':
			return 6, index + 1
		default:
			return 1, index + 1
		}
	}
	runeValue, width := utf8.DecodeRuneInString(value[index:])
	if width <= 0 {
		return 6, index + 1
	}
	if (width == 1 && runeValue == utf8.RuneError) || runeValue == '\u2028' || runeValue == '\u2029' {
		return 6, index + width
	}
	return uint64(width), index + width
}

func hasCustomJSONEncoding(typeOf reflect.Type) bool {
	jsonMarshaler := reflect.TypeFor[json.Marshaler]()
	textMarshaler := reflect.TypeFor[interface{ MarshalText() ([]byte, error) }]()
	if typeOf.Implements(jsonMarshaler) || typeOf.Implements(textMarshaler) {
		return true
	}
	return typeOf.Kind() != reflect.Pointer && (reflect.PointerTo(typeOf).Implements(jsonMarshaler) || reflect.PointerTo(typeOf).Implements(textMarshaler))
}

func hasUnexportedDynamicFields(typeOf reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[typeOf] {
		return false
	}
	seen[typeOf] = true
	if typeOf.Kind() != reflect.Struct {
		return false
	}
	for field := range typeOf.Fields() {
		if field.PkgPath != "" && typeContainsDynamicData(field.Type, make(map[reflect.Type]bool)) {
			return true
		}
		if field.PkgPath == "" && hasUnexportedDynamicFields(field.Type, seen) {
			return true
		}
	}
	return false
}

func typeContainsDynamicData(typeOf reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[typeOf] {
		return false
	}
	seen[typeOf] = true
	switch typeOf.Kind() {
	case reflect.String, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return typeContainsDynamicData(typeOf.Elem(), seen)
	case reflect.Struct:
		for field := range typeOf.Fields() {
			if typeContainsDynamicData(field.Type, seen) {
				return true
			}
		}
		return false
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return false
	}
	return false
}

func hasOption(options, want string) bool {
	return slices.Contains(strings.Split(options, ","), want)
}

func isJSONEmpty(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool:
		return !value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return value.IsNil()
	case reflect.Invalid, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.Struct, reflect.UnsafePointer:
		return false
	default:
		return false
	}
}

func detachStreamValue(value reflect.Value, depth int) (reflect.Value, error) {
	if depth > maxStreamValueDepth {
		return reflect.Value{}, fmt.Errorf("%w: nesting exceeds %d levels", errUnsupportedStreamValue, maxStreamValueDepth)
	}
	if !value.IsValid() {
		return reflect.Value{}, nil
	}
	typeOf := value.Type()
	if hasCustomJSONEncoding(typeOf) {
		return reflect.Value{}, fmt.Errorf("%w: custom JSON encoding on %s", errUnsupportedStreamValue, typeOf)
	}
	switch value.Kind() {
	case reflect.Interface:
		return detachInterface(value, depth)
	case reflect.Pointer:
		return detachPointer(value, depth)
	case reflect.String:
		out := reflect.New(typeOf).Elem()
		out.SetString(strings.Clone(value.String()))
		return out, nil
	case reflect.Slice:
		return detachSlice(value, depth)
	case reflect.Array:
		return detachArray(value, depth)
	case reflect.Map:
		return detachMap(value, depth)
	case reflect.Struct:
		return detachStruct(value, depth)
	case reflect.Invalid:
		return reflect.Value{}, nil
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return value, nil
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return reflect.Value{}, fmt.Errorf("%w: %s", errUnsupportedStreamValue, typeOf)
	default:
		return value, nil
	}
}

func detachInterface(value reflect.Value, depth int) (reflect.Value, error) {
	out := reflect.New(value.Type()).Elem()
	if value.IsNil() {
		return out, nil
	}
	cloned, err := detachStreamValue(value.Elem(), depth+1)
	if err != nil {
		return reflect.Value{}, err
	}
	out.Set(cloned)
	return out, nil
}

func detachPointer(value reflect.Value, depth int) (reflect.Value, error) {
	if value.IsNil() {
		return value, nil
	}
	out := reflect.New(value.Type().Elem())
	cloned, err := detachStreamValue(value.Elem(), depth+1)
	if err != nil {
		return reflect.Value{}, err
	}
	out.Elem().Set(cloned)
	return out, nil
}

func detachSlice(value reflect.Value, depth int) (reflect.Value, error) {
	if value.IsNil() {
		return value, nil
	}
	typeOf := value.Type()
	out := reflect.MakeSlice(typeOf, value.Len(), value.Len())
	if isScalarStreamKind(typeOf.Elem().Kind()) {
		reflect.Copy(out, value)
		return out, nil
	}
	for index := range value.Len() {
		cloned, err := detachStreamValue(value.Index(index), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		out.Index(index).Set(cloned)
	}
	return out, nil
}

func detachArray(value reflect.Value, depth int) (reflect.Value, error) {
	typeOf := value.Type()
	out := reflect.New(typeOf).Elem()
	out.Set(value)
	if isScalarStreamKind(typeOf.Elem().Kind()) {
		return out, nil
	}
	for index := range value.Len() {
		cloned, err := detachStreamValue(value.Index(index), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		out.Index(index).Set(cloned)
	}
	return out, nil
}

func detachMap(value reflect.Value, depth int) (reflect.Value, error) {
	if value.IsNil() {
		return value, nil
	}
	out := reflect.MakeMapWithSize(value.Type(), value.Len())
	iter := value.MapRange()
	for iter.Next() {
		clonedKey, err := detachStreamValue(iter.Key(), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		clonedValue, err := detachStreamValue(iter.Value(), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		out.SetMapIndex(clonedKey, clonedValue)
	}
	return out, nil
}

func detachStruct(value reflect.Value, depth int) (reflect.Value, error) {
	typeOf := value.Type()
	if hasUnexportedDynamicFields(typeOf, make(map[reflect.Type]bool)) {
		return reflect.Value{}, fmt.Errorf("%w: %s has unexported dynamic fields", errUnsupportedStreamValue, typeOf)
	}
	out := reflect.New(typeOf).Elem()
	out.Set(value)
	// json:"-" fields are still detached because the copied struct is passed
	// downstream; unsupported dynamic values in those fields remain rejected.
	for index := range typeOf.NumField() {
		fieldType := typeOf.Field(index)
		if fieldType.PkgPath != "" {
			continue
		}
		cloned, err := detachStreamValue(value.Field(index), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		out.Field(index).Set(cloned)
	}
	return out, nil
}

func isScalarStreamKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	case reflect.Invalid, reflect.Array, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Pointer, reflect.Slice, reflect.String, reflect.Struct, reflect.UnsafePointer:
		return false
	default:
		return false
	}
}
