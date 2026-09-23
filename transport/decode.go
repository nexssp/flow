package transport

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/validation"
)

// Decode maps a raw string map into a typed options struct.
//
//	flow:"key"        key name in the @require block
//	default:"..."     value used when the key is absent
//	validate:"..."    ecosystem validator tag (required, url, min, …)
//
// Unknown keys are rejected. Values that fail `validate` are returned
// as xerr.Validation with structured details.
func Decode[T any](ctx context.Context, raw map[string]string) (T, error) {
	var out T
	v := reflect.ValueOf(&out).Elem()
	t := v.Type()

	used := make(map[string]bool, len(raw))

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		key := f.Tag.Get("flow")
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

	if err := validation.Struct(ctx, &out); err != nil {
		return out, validation.FromValidatorError(err)
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
	default:
		return fmt.Errorf("unsupported field type %s", field.Kind())
	}
	return nil
}

func knownKeys(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		if k := t.Field(i).Tag.Get("flow"); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// Bind populates T from raw options, resolving env tags and defaults, validates, and invokes factory.
func Bind[T any](ctx context.Context, raw map[string]string, factory func(T) (action.Library, error)) (action.Library, error) {
	var config T
	reflectedValue := reflect.ValueOf(&config).Elem()
	reflectedType := reflectedValue.Type()

	mergedRaw := make(map[string]string, len(raw))
	for key, value := range raw {
		mergedRaw[key] = value
	}

	for i := 0; i < reflectedType.NumField(); i++ {
		field := reflectedType.Field(i)
		flowKey := field.Tag.Get("flow")
		envKey := field.Tag.Get("env")

		if flowKey != "" && mergedRaw[flowKey] == "" && envKey != "" {
			if envValue := os.Getenv(envKey); envValue != "" {
				mergedRaw[flowKey] = envValue
			}
		}
	}

	decoded, err := Decode[T](ctx, mergedRaw)
	if err != nil {
		return action.Library{}, err
	}

	return factory(decoded)
}
