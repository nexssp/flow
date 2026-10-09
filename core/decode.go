package core

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Decode maps a string map to a typed options struct. This is
// the contract that each external Bundle uses to read
// its @require block.
//
// Tags:
//
//	nflow:"key"     name of the key in the @require block
//	default:"..."   value when the key is missing
//
// Unknown keys are rejected — a typo in .nflow should cause a loud
// failure during loading, not silently default.
//
// Supported field types: string, bool, int, int64, time.Duration,
// float64. Any other type is a design error for the Bundle.
func Decode[T any](raw map[string]string) (T, error) {
	var out T
	v := reflect.ValueOf(&out).Elem()
	t := v.Type()

	used := make(map[string]bool, len(raw))

	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		key := f.Tag.Get("nflow")
		if key == "" {
			continue
		}

		rawVal, present := raw[key]
		if present {
			used[key] = true
		}
		if !present || rawVal == "" {
			if def := f.Tag.Get("default"); def != "" {
				rawVal = def
			} else {
				continue
			}
		}
		if err := setField(v.Field(i), rawVal); err != nil {
			return out, fmt.Errorf("option %q: %w", key, err)
		}
	}

	for k := range raw {
		if !used[k] {
			return out, fmt.Errorf(
				"unknown option %q (allowed: %s)",
				k, strings.Join(knownKeys(t), ", "))
		}
	}
	return out, nil
}

func setField(field reflect.Value, val string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(val)
	case reflect.Bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Int, reflect.Int64:
		if field.Type() == reflect.TypeFor[time.Duration]() {
			d, err := time.ParseDuration(val)
			if err != nil {
				return err
			}
			field.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(n)
	case reflect.Float64:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return err
		}
		field.SetFloat(f)
	case reflect.Invalid, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr, reflect.Float32,
		reflect.Complex64, reflect.Complex128, reflect.Array,
		reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice, reflect.Struct,
		reflect.UnsafePointer:
		return fmt.Errorf("unsupported field type %s", field.Kind())
	}
	return nil
}

func knownKeys(t reflect.Type) []string {
	var out []string
	for field := range t.Fields() {
		if k := field.Tag.Get("nflow"); k != "" {
			out = append(out, k)
		}
	}
	return out
}
