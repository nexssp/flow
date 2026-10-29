package modifiers_meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/validation"

	compiler "github.com/nexssp/flow/compiler"
)

func exampleModifier() compiler.Modifier {
	return compiler.WithUnique(compiler.Modifier{
		Name:      "example",
		ValueKind: compiler.ModifierKindJSON,
		Example:   `:example='{"account_id":"acct-demo"}'`,
		ApplyTarget: func(target action.AnyAction, builder *action.Builder[any, any], raw string) error {
			var requestSchema any
			if typed, ok := target.(action.TypedPayload); ok {
				requestSchema = typed.ReqPayload()
			}

			example, err := decodeExample(raw, requestSchema)
			if err != nil {
				name := "action"
				if target != nil && target.Describe() != nil && target.Describe().Name != "" {
					name = target.Describe().Name
				}
				return fmt.Errorf("%s request: %w", name, err)
			}

			// Example is documentation metadata only: do not invoke target or
			// alter its execution path.
			builder.Example(example)
			return nil
		},
	})
}

func decodeExample(raw string, requestSchema any) (any, error) {
	typ := reflect.TypeOf(requestSchema)
	if typ == nil {
		var generic any
		if err := json.Unmarshal([]byte(raw), &generic); err != nil {
			return nil, fmt.Errorf("decode JSON: %w", err)
		}
		return generic, nil
	}

	value := reflect.New(typ)
	decoder := json.NewDecoder(strings.NewReader(raw))
	base := typ
	for base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	if base.Kind() == reflect.Struct {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(value.Interface()); err != nil {
		return nil, fmt.Errorf("does not match request type %s: %w", base, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("expected one JSON value")
		}
		return nil, fmt.Errorf("decode trailing JSON: %w", err)
	}

	example := value.Elem().Interface()
	if base.Kind() == reflect.Struct {
		// validation.Struct trims strings in place, so validate a separate
		// decode and keep Meta.Example exactly as the author supplied it.
		validationValue := reflect.New(typ)
		if err := json.Unmarshal([]byte(raw), validationValue.Interface()); err != nil {
			return nil, fmt.Errorf("decode validation copy: %w", err)
		}
		if err := validation.Struct(context.Background(), validationValue.Elem().Interface()); err != nil {
			return nil, fmt.Errorf("does not satisfy request validation: %w", err)
		}
	}
	return example, nil
}
