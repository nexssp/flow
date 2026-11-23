package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
)

const Redacted = "[REDACTED]"

type Value struct {
	value string
}

var valueType = reflect.TypeFor[Value]()

func New(value string) Value {
	return Value{value: value}
}

func IsEmpty(value Value) bool { return value.value == "" }

// WithValue exposes the secret bytes only for the duration of fn.
func WithValue(value Value, fn func([]byte) error) error {
	if fn == nil {
		return errors.New("secret: value callback is nil")
	}
	return fn([]byte(value.value))
}

func IsValue(value any) bool {
	if value == nil {
		return false
	}
	t := reflect.TypeOf(value)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t == valueType
}

func IsSecretType(t reflect.Type) bool {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t == valueType
}

// WithoutSecrets returns a static type containing the public fields that can
// remain in an inherited payload after Secret-bearing fields are removed.
func WithoutSecrets(t reflect.Type) reflect.Type {
	if t == nil || !TypeContains(t) {
		return t
	}
	if IsSecretType(t) {
		return nil
	}
	//nolint:exhaustive // Only struct and container kinds can carry nested Secret fields.
	switch t.Kind() {
	case reflect.Struct:
		fields := make([]reflect.StructField, 0, t.NumField())
		for field := range t.Fields() {
			if field.PkgPath != "" || TypeContains(field.Type) {
				continue
			}
			fields = append(fields, reflect.StructField{Name: field.Name, Type: field.Type, Tag: field.Tag, Anonymous: field.Anonymous})
		}
		return reflect.StructOf(fields)
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Array:
		return nil
	default:
		return nil
	}
}

// WireType replaces each Secret leaf with its string wire shape for structural
// JSON checks. Flow values remain Secret.Value and still require FitsType.
func WireType(t reflect.Type) reflect.Type {
	if t == nil || !TypeContains(t) {
		return t
	}
	if IsSecretType(t) {
		return reflect.TypeFor[string]()
	}
	//nolint:exhaustive // Only structural kinds need recursive wire-shape projection.
	switch t.Kind() {
	case reflect.Pointer:
		return reflect.PointerTo(WireType(t.Elem()))
	case reflect.Map:
		return reflect.MapOf(WireType(t.Key()), WireType(t.Elem()))
	case reflect.Slice:
		return reflect.SliceOf(WireType(t.Elem()))
	case reflect.Array:
		return reflect.ArrayOf(t.Len(), WireType(t.Elem()))
	case reflect.Struct:
		fields := make([]reflect.StructField, 0, t.NumField())
		for field := range t.Fields() {
			if field.PkgPath != "" {
				continue
			}
			fields = append(fields, reflect.StructField{
				Name: field.Name, Type: WireType(field.Type), Tag: field.Tag, Anonymous: field.Anonymous,
			})
		}
		return reflect.StructOf(fields)
	default:
		return t
	}
}

func TypeContains(t reflect.Type) bool {
	return typeContains(t, make(map[reflect.Type]bool))
}

func typeContains(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == nil {
		return false
	}
	if IsSecretType(t) {
		return true
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	//nolint:exhaustive // Non-container kinds cannot contain nested Secret values.
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return typeContains(t.Elem(), seen)
	case reflect.Map:
		return typeContains(t.Key(), seen) || typeContains(t.Elem(), seen)
	case reflect.Struct:
		for field := range t.Fields() {
			if field.PkgPath == "" && typeContains(field.Type, seen) {
				return true
			}
		}
	}
	return false
}

func Contains(value any) bool {
	return valueContains(reflect.ValueOf(value), make(map[visit]bool))
}

type visit struct {
	t reflect.Type
	p uintptr
}

//nolint:gocyclo // Reflective traversal handles cycles and each container shape explicitly.
func valueContains(value reflect.Value, seen map[visit]bool) bool {
	if !value.IsValid() {
		return false
	}
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	if IsSecretType(value.Type()) {
		return true
	}
	//nolint:exhaustive // Only reflective containers can contain nested Secret values.
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return false
		}
		key := visit{t: value.Type(), p: value.Pointer()}
		if seen[key] {
			return false
		}
		seen[key] = true
		return valueContains(value.Elem(), seen)
	case reflect.Map:
		if value.IsNil() {
			return false
		}
		key := visit{t: value.Type(), p: uintptr(value.UnsafePointer())}
		if seen[key] {
			return false
		}
		seen[key] = true
		iter := value.MapRange()
		for iter.Next() {
			if valueContains(iter.Key(), seen) || valueContains(iter.Value(), seen) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice {
			if value.IsNil() {
				return false
			}
			key := visit{t: value.Type(), p: value.Pointer()}
			if seen[key] {
				return false
			}
			seen[key] = true
		}
		for index := range value.Len() {
			if valueContains(value.Index(index), seen) {
				return true
			}
		}
	case reflect.Struct:
		for field := range value.Type().Fields() {
			if field.PkgPath == "" && valueContains(value.FieldByIndex(field.Index), seen) {
				return true
			}
		}
	}
	return false
}

// FitsType reports whether every statically known secret field in source has an
// exactly corresponding secret field in target. Generic interfaces are not a
// valid destination for a secret.
//
//nolint:gocyclo // Recursive compatibility checks cover each Secret-bearing container shape.
func FitsType(source, target reflect.Type) bool {
	if !TypeContains(source) && !TypeContains(target) {
		return true
	}
	if source == nil || target == nil {
		return false
	}
	if IsSecretType(source) || IsSecretType(target) {
		return IsSecretType(source) && IsSecretType(target)
	}
	if source.Kind() == reflect.Pointer || target.Kind() == reflect.Pointer {
		if source.Kind() != target.Kind() {
			return false
		}
		return FitsType(source.Elem(), target.Elem())
	}
	//nolint:exhaustive // Only structs, maps, slices, and arrays can retain typed Secret fields.
	switch source.Kind() {
	case reflect.Struct:
		if target.Kind() != reflect.Struct {
			return false
		}
		for sourceField := range source.Fields() {
			if sourceField.PkgPath != "" {
				continue
			}
			targetField, ok := matchingField(target, fieldName(sourceField))
			sourceSensitive := TypeContains(sourceField.Type)
			targetSensitive := ok && TypeContains(targetField.Type)
			if !sourceSensitive && !targetSensitive {
				continue
			}
			if sourceSensitive && !ok || ok && !FitsType(sourceField.Type, targetField.Type) {
				return false
			}
		}
		return true
	case reflect.Map:
		if target.Kind() != reflect.Map || source.Key() != target.Key() {
			return false
		}
		return FitsType(source.Elem(), target.Elem())
	case reflect.Slice, reflect.Array:
		return (target.Kind() == reflect.Slice || target.Kind() == reflect.Array) && FitsType(source.Elem(), target.Elem())
	default:
		return false
	}
}

// FitsValue is the runtime counterpart to FitsType. It checks the actual
// values of dynamic maps and slices before they enter an action.
func FitsValue(value any, target reflect.Type) bool {
	if !Contains(value) && !TypeContains(target) {
		return true
	}
	return fitsValue(reflect.ValueOf(value), target)
}

//nolint:gocyclo // Runtime validation checks dynamic maps and typed containers fail-closed.
func fitsValue(source reflect.Value, target reflect.Type) bool {
	if !source.IsValid() || target == nil {
		return false
	}
	for source.Kind() == reflect.Interface {
		if source.IsNil() {
			return false
		}
		source = source.Elem()
	}
	if IsSecretType(source.Type()) || IsSecretType(target) {
		return IsSecretType(source.Type()) && IsSecretType(target)
	}
	if source.Kind() == reflect.Pointer || target.Kind() == reflect.Pointer {
		if source.Kind() != target.Kind() || source.IsNil() {
			return false
		}
		return fitsValue(source.Elem(), target.Elem())
	}
	//nolint:exhaustive // Non-container kinds reject Secret-bearing values below.
	switch source.Kind() {
	case reflect.Map:
		if target.Kind() == reflect.Struct {
			iter := source.MapRange()
			for iter.Next() {
				if iter.Key().Kind() != reflect.String {
					if Contains(iter.Key().Interface()) || Contains(iter.Value().Interface()) {
						return false
					}
					continue
				}
				field, ok := matchingField(target, iter.Key().String())
				containsSecret := Contains(iter.Value().Interface())
				if !ok {
					if containsSecret {
						return false
					}
					continue
				}
				if (containsSecret || TypeContains(field.Type)) && !fitsValue(iter.Value(), field.Type) {
					return false
				}
			}
			return true
		}
		if target.Kind() != reflect.Map || source.Type().Key() != target.Key() {
			return false
		}
		iter := source.MapRange()
		for iter.Next() {
			if Contains(iter.Key().Interface()) && !fitsValue(iter.Key(), target.Key()) {
				return false
			}
			if (Contains(iter.Value().Interface()) || TypeContains(target.Elem())) && !fitsValue(iter.Value(), target.Elem()) {
				return false
			}
		}
		return true
	case reflect.Struct:
		if target.Kind() != reflect.Struct {
			return false
		}
		for sourceIndex := range source.NumField() {
			sourceField := source.Type().Field(sourceIndex)
			if sourceField.PkgPath != "" {
				continue
			}
			targetField, ok := matchingField(target, fieldName(sourceField))
			sourceSensitive := Contains(source.Field(sourceIndex).Interface())
			targetSensitive := ok && TypeContains(targetField.Type)
			if !sourceSensitive && !targetSensitive {
				continue
			}
			if sourceSensitive && !ok || ok && !fitsValue(source.Field(sourceIndex), targetField.Type) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
			return false
		}
		for index := range source.Len() {
			if (Contains(source.Index(index).Interface()) || TypeContains(target.Elem())) && !fitsValue(source.Index(index), target.Elem()) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func matchingField(t reflect.Type, name string) (reflect.StructField, bool) {
	for field := range t.Fields() {
		if field.PkgPath != "" {
			continue
		}
		if fieldName(field) == name || field.Name == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func fieldName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if name != "" && name != "-" {
		return name
	}
	return field.Name
}

// Sanitize returns a value safe for observers and event sinks. Secret leaves
// become the redaction marker; other values are preserved where possible.
func Sanitize(value any) any {
	return sanitize(reflect.ValueOf(value), make(map[visit]bool))
}

// SanitizeWithSources redacts Secret leaves and any echoed secret contents in
// a value before it leaves the execution boundary for an observer or sink.
func SanitizeWithSources(value any, sources ...any) any {
	var needles []string
	for _, source := range sources {
		if Contains(source) {
			needles = append(needles, secretStrings(reflect.ValueOf(source))...)
		}
	}
	if len(needles) == 0 {
		return Sanitize(value)
	}
	return SanitizeWithSecretValues(value, needles...)
}

// SanitizeWithSecretValues redacts known secret strings from JSON-compatible
// values. It is used for raw wire payloads before a schema has materialized
// Secret.Value instances.
func SanitizeWithSecretValues(value any, secrets ...string) any {
	if len(secrets) == 0 {
		return Sanitize(value)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return Redacted
	}
	var sanitized any
	if err := json.Unmarshal(raw, &sanitized); err != nil {
		return Redacted
	}
	return replaceSecretStrings(sanitized, secrets)
}

const (
	maxScrubberSecrets = 1024
	maxScrubberBytes   = 1 << 20
)

// Scrubber remembers bounded Secret values for one execution so echoed values
// can be removed from outputs and errors that no longer retain Secret.Value.
type Scrubber struct {
	mu       sync.RWMutex
	values   []string
	seen     map[string]struct{}
	size     int
	overflow bool
}

// NewScrubber creates an empty execution-scoped Secret scrubber.
func NewScrubber() *Scrubber {
	return &Scrubber{seen: make(map[string]struct{})}
}

// Capture records typed Secret values, or only the fields declared Secret by a
// target schema. Schema-directed capture avoids traversing ordinary sibling data.
func (scrubber *Scrubber) Capture(value any, target reflect.Type) {
	if scrubber == nil {
		return
	}
	var values []string
	if target == nil {
		values = secretStrings(reflect.ValueOf(value))
	} else if TypeContains(target) {
		values = boundarySecretStrings(reflect.ValueOf(value), target)
	}
	scrubber.add(values)
}

// Sanitize redacts all captured values from value. If the bounded capture limit
// is exceeded, it redacts the complete result rather than risk an incomplete scrub.
func (scrubber *Scrubber) Sanitize(value any) any {
	if scrubber == nil {
		return Sanitize(value)
	}
	values, overflow := scrubber.snapshot()
	if overflow {
		return Redacted
	}
	if len(values) == 0 {
		if Contains(value) {
			return Sanitize(value)
		}
		return value
	}
	if text, ok := value.(string); ok {
		for _, secretValue := range values {
			if secretValue != "" && strings.Contains(text, secretValue) {
				text = strings.ReplaceAll(text, secretValue, Redacted)
			}
		}
		return text
	}
	return SanitizeWithSecretValues(value, values...)
}

// SanitizeError removes captured Secret values from err without exposing its
// original error chain when a value was changed.
func (scrubber *Scrubber) SanitizeError(err error) error {
	if err == nil {
		return nil
	}
	if scrubber == nil {
		return err
	}
	values, overflow := scrubber.snapshot()
	if overflow {
		return safeError{message: "operation failed while handling a secret value"}
	}
	return SanitizeErrorWithSecretValues(err, values...)
}

// Clear releases references to captured values after the execution is complete.
func (scrubber *Scrubber) Clear() {
	if scrubber == nil {
		return
	}
	scrubber.mu.Lock()
	clear(scrubber.values)
	scrubber.values = nil
	scrubber.seen = make(map[string]struct{})
	scrubber.size = 0
	scrubber.overflow = false
	scrubber.mu.Unlock()
}

func (scrubber *Scrubber) add(values []string) {
	scrubber.mu.Lock()
	defer scrubber.mu.Unlock()
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := scrubber.seen[value]; exists {
			continue
		}
		if len(scrubber.values) >= maxScrubberSecrets || scrubber.size+len(value) > maxScrubberBytes {
			scrubber.overflow = true
			continue
		}
		owned := strings.Clone(value)
		scrubber.seen[owned] = struct{}{}
		scrubber.values = append(scrubber.values, owned)
		scrubber.size += len(owned)
	}
}

func (scrubber *Scrubber) snapshot() ([]string, bool) {
	scrubber.mu.RLock()
	defer scrubber.mu.RUnlock()
	return append([]string(nil), scrubber.values...), scrubber.overflow
}

// SanitizeBoundary redacts fields declared as Secret in a schema contract,
// including plain wire strings observed before request materialization.
func SanitizeBoundary(value any, target reflect.Type) (sanitized any, secrets []string) {
	secrets = boundarySecretStrings(reflect.ValueOf(value), target)
	raw, err := json.Marshal(value)
	if err != nil {
		return Redacted, secrets
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Redacted, secrets
	}
	sanitized = redactBoundaryFields(decoded, target)
	return SanitizeWithSecretValues(sanitized, secrets...), secrets
}

func replaceSecretStrings(value any, secrets []string) any {
	switch current := value.(type) {
	case string:
		for _, secretValue := range secrets {
			if secretValue != "" {
				current = strings.ReplaceAll(current, secretValue, Redacted)
			}
		}
		return current
	case []any:
		for index := range current {
			current[index] = replaceSecretStrings(current[index], secrets)
		}
		return current
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, item := range current {
			cleanKey := key
			for _, secretValue := range secrets {
				if secretValue != "" {
					cleanKey = strings.ReplaceAll(cleanKey, secretValue, Redacted)
				}
			}
			result[cleanKey] = replaceSecretStrings(item, secrets)
		}
		return result
	default:
		return current
	}
}

//nolint:gocyclo // Recursively maps a declared schema shape to its raw wire Secret values.
func boundarySecretStrings(source reflect.Value, target reflect.Type) []string {
	if !source.IsValid() || target == nil {
		return nil
	}
	for source.Kind() == reflect.Interface {
		if source.IsNil() {
			return nil
		}
		source = source.Elem()
	}
	if IsSecretType(target) {
		if IsSecretType(source.Type()) {
			return secretStrings(source)
		}
		if source.Kind() == reflect.String && source.String() != "" {
			return []string{source.String()}
		}
		if source.Kind() == reflect.Slice && source.Type().Elem().Kind() == reflect.Uint8 && source.Len() > 0 {
			return []string{string(source.Bytes())}
		}
		return nil
	}
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if source.Kind() == reflect.Pointer {
		if source.IsNil() {
			return nil
		}
		source = source.Elem()
	}
	var out []string
	//nolint:exhaustive // Only schema container kinds can hold nested Secret fields.
	switch target.Kind() {
	case reflect.Struct:
		for field := range target.Fields() {
			if field.PkgPath != "" || !TypeContains(field.Type) {
				continue
			}
			var child reflect.Value
			//nolint:exhaustive // Only maps and structs can supply fields for a struct schema.
			switch source.Kind() {
			case reflect.Map:
				if source.Type().Key().Kind() != reflect.String {
					continue
				}
				key := reflect.ValueOf(fieldName(field))
				if key.Type() != source.Type().Key() {
					if !key.Type().ConvertibleTo(source.Type().Key()) {
						continue
					}
					key = key.Convert(source.Type().Key())
				}
				child = source.MapIndex(key)
			case reflect.Struct:
				sourceField, ok := matchingField(source.Type(), fieldName(field))
				if !ok {
					continue
				}
				child = source.FieldByIndex(sourceField.Index)
			default:
				continue
			}
			out = append(out, boundarySecretStrings(child, field.Type)...)
		}
	case reflect.Map:
		if source.Kind() == reflect.Map {
			iter := source.MapRange()
			for iter.Next() {
				if TypeContains(target.Key()) {
					out = append(out, boundarySecretStrings(iter.Key(), target.Key())...)
				}
				if TypeContains(target.Elem()) {
					out = append(out, boundarySecretStrings(iter.Value(), target.Elem())...)
				}
			}
		}
	case reflect.Slice, reflect.Array:
		if source.Kind() == reflect.Slice || source.Kind() == reflect.Array {
			for index := range source.Len() {
				out = append(out, boundarySecretStrings(source.Index(index), target.Elem())...)
			}
		}
	}
	return out
}

//nolint:gocyclo // Fail-closed redaction follows the supported JSON schema container shapes.
func redactBoundaryFields(value any, target reflect.Type) any {
	if target == nil {
		return value
	}
	if IsSecretType(target) {
		return Redacted
	}
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	//nolint:exhaustive // Only struct/map/slice/array payloads can expose declared Secret fields.
	switch target.Kind() {
	case reflect.Struct:
		values, ok := value.(map[string]any)
		if !ok {
			if TypeContains(target) {
				return Redacted
			}
			return value
		}
		for field := range target.Fields() {
			if field.PkgPath != "" || !TypeContains(field.Type) {
				continue
			}
			name := fieldName(field)
			if current, exists := values[name]; exists {
				values[name] = redactBoundaryFields(current, field.Type)
			}
		}
		return values
	case reflect.Map:
		values, ok := value.(map[string]any)
		if !ok {
			if TypeContains(target) {
				return Redacted
			}
			return value
		}
		if TypeContains(target.Key()) {
			return Redacted
		}
		if TypeContains(target.Elem()) {
			for key, current := range values {
				values[key] = redactBoundaryFields(current, target.Elem())
			}
		}
		return values
	case reflect.Slice, reflect.Array:
		values, ok := value.([]any)
		if !ok {
			if TypeContains(target) {
				return Redacted
			}
			return value
		}
		if TypeContains(target.Elem()) {
			for index := range values {
				values[index] = redactBoundaryFields(values[index], target.Elem())
			}
		}
		return values
	default:
		return value
	}
}

//nolint:gocyclo // Sanitization handles cycles and each reflective container shape explicitly.
func sanitize(value reflect.Value, seen map[visit]bool) any {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if IsSecretType(value.Type()) {
		return Redacted
	}
	//nolint:exhaustive // All other reflect kinds are returned unchanged below.
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		key := visit{t: value.Type(), p: value.Pointer()}
		if seen[key] {
			return Redacted
		}
		seen[key] = true
		return sanitize(value.Elem(), seen)
	case reflect.Map:
		if value.IsNil() {
			return value.Interface()
		}
		key := visit{t: value.Type(), p: uintptr(value.UnsafePointer())}
		if seen[key] {
			return Redacted
		}
		seen[key] = true
		if !Contains(value.Interface()) {
			return value.Interface()
		}
		if value.Type().Key().Kind() != reflect.String {
			return Redacted
		}
		result := make(map[string]any, value.Len())
		iter := value.MapRange()
		for iter.Next() {
			result[iter.Key().String()] = sanitize(iter.Value(), seen)
		}
		return result
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return value.Interface()
		}
		if value.Kind() == reflect.Slice {
			key := visit{t: value.Type(), p: value.Pointer()}
			if seen[key] {
				return Redacted
			}
			seen[key] = true
		}
		if !Contains(value.Interface()) {
			return value.Interface()
		}
		result := make([]any, value.Len())
		for index := range value.Len() {
			result[index] = sanitize(value.Index(index), seen)
		}
		return result
	case reflect.Struct:
		if !TypeContains(value.Type()) {
			return value.Interface()
		}
		result := make(map[string]any, value.NumField())
		for index := range value.NumField() {
			field := value.Type().Field(index)
			if field.PkgPath != "" || field.Tag.Get("json") == "-" {
				continue
			}
			name := fieldName(field)
			result[name] = sanitize(value.Field(index), seen)
		}
		return result
	default:
		return value.Interface()
	}
}

// SanitizeError creates an observer-safe error when the associated payload
// contains a Secret. It deliberately does not unwrap the original error.
func SanitizeError(err error, values ...any) error {
	if err == nil {
		return nil
	}
	contains := false
	message := err.Error()
	for _, value := range values {
		if !Contains(value) {
			continue
		}
		contains = true
		for _, raw := range secretStrings(reflect.ValueOf(value)) {
			if raw != "" {
				message = strings.ReplaceAll(message, raw, Redacted)
			}
		}
	}
	if !contains {
		return err
	}
	return safeError{message: message}
}

// SanitizeErrorWithSecretValues removes raw wire Secret strings from an error
// associated with a request before those strings have been wrapped as Values.
func SanitizeErrorWithSecretValues(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	changed := false
	for _, secretValue := range secrets {
		if secretValue == "" || !strings.Contains(message, secretValue) {
			continue
		}
		message = strings.ReplaceAll(message, secretValue, Redacted)
		changed = true
	}
	if !changed {
		return err
	}
	return safeError{message: message}
}

type safeError struct {
	message string
}

func (err safeError) Error() string { return err.message }

func secretStrings(value reflect.Value) []string {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if IsSecretType(value.Type()) {
		if value.Type() == valueType && value.CanInterface() {
			secretValue, ok := reflect.TypeAssert[Value](value)
			if !ok {
				return nil
			}
			return []string{secretValue.value}
		}
		if value.Kind() == reflect.Pointer && !value.IsNil() {
			return secretStrings(value.Elem())
		}
		return nil
	}
	var out []string
	//nolint:exhaustive // Only containers can hold nested Secret values.
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			out = append(out, secretStrings(value.Elem())...)
		}
	case reflect.Map:
		if !value.IsNil() {
			iter := value.MapRange()
			for iter.Next() {
				out = append(out, secretStrings(iter.Key())...)
				out = append(out, secretStrings(iter.Value())...)
			}
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			out = append(out, secretStrings(value.Index(index))...)
		}
	case reflect.Struct:
		for index := range value.NumField() {
			field := value.Type().Field(index)
			if field.PkgPath == "" {
				out = append(out, secretStrings(value.Field(index))...)
			}
		}
	}
	return out
}

func (Value) Format(state fmt.State, verb rune) {
	if verb == 'q' {
		if _, err := fmt.Fprintf(state, "%q", Redacted); err != nil {
			return
		}
		return
	}
	if _, err := io.WriteString(state, Redacted); err != nil {
		return
	}
}

func (Value) MarshalJSON() ([]byte, error) { return json.Marshal(Redacted) }

func (Value) MarshalText() ([]byte, error) { return []byte(Redacted), nil }

func (Value) LogValue() slog.Value { return slog.StringValue(Redacted) }

func (*Value) UnmarshalJSON([]byte) error {
	return errors.New("secret: serialized values cannot be restored; supply the secret again")
}

func (*Value) UnmarshalText([]byte) error {
	return errors.New("secret: serialized values cannot be restored; supply the secret again")
}
