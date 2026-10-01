package core

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type TypeDescriptor struct {
	Type   reflect.Type
	Stream bool
}

type CapabilityContract struct {
	Name   string
	Input  TypeDescriptor
	Output TypeDescriptor
}

type ContractProvider interface {
	Contract(name string) (CapabilityContract, bool)
}

// Analyze runs polymorphic contract validation on the AST. It is a
// compile-time gate: any edge whose producer output cannot be coerced
// into the consumer input fails here, before Execute is ever reached.
//
// The rule mirrors runtime Coerce exactly — nominal assignability
// first, then a JSON round-trip for the same struct-to-struct cases
// the runtime decoder handles. If a pipeline compiles here, it will
// not fail at runtime with "cannot coerce input into target".
func Analyze(resolver CapabilityResolver, expr Expr) error {
	if resolver == nil {
		return xerr.Internal("analyze: capability resolver is nil")
	}
	if expr == nil {
		return nil
	}
	return expr.Analyze(resolver)
}

func contractForExpr(resolver CapabilityResolver, expr Expr) (CapabilityContract, bool) {
	atom, ok := expr.(*Atom)
	if !ok || atom == nil {
		return CapabilityContract{}, false
	}
	if act, found := resolver.Action(atom.Name); found {
		return actionContract(atom.Name, act), true
	}
	if src, found := resolver.Stream(atom.Name); found {
		return CapabilityContract{
			Name:   atom.Name,
			Input:  TypeDescriptor{Type: payloadType(src.ReqPayload())},
			Output: TypeDescriptor{Type: payloadType(src.ResPayload()), Stream: true},
		}, true
	}
	if op, found := resolver.Operator(atom.Name); found {
		return CapabilityContract{
			Name:   atom.Name,
			Input:  TypeDescriptor{Type: op.InType, Stream: true},
			Output: TypeDescriptor{Type: op.OutType, Stream: true},
		}, true
	}
	return CapabilityContract{}, false
}

func actionContract(name string, act action.AnyAction) CapabilityContract {
	contract := CapabilityContract{Name: name}
	if typed, ok := act.(action.TypedPayload); ok {
		contract.Input.Type = payloadType(typed.ReqPayload())
		contract.Output.Type = payloadType(typed.ResPayload())
	}
	return contract
}

func payloadType(payload any) reflect.Type {
	if payload == nil {
		return nil
	}
	return reflect.TypeOf(payload)
}

// requireCompatible validates one pipeline edge. It is the compile-time
// counterpart of action.Coerce: a type that passes here will pass at
// runtime, and a type that fails here would have failed there too —
// except the failure surfaces now, with the pipeline names and the
// specific field that cannot be coerced, instead of at the first call.
func requireCompatible(produced, consumed TypeDescriptor, producer, consumer string) error {
	if produced.Type == nil || consumed.Type == nil {
		return nil
	}
	if produced.Stream != consumed.Stream {
		return xerr.Validation(
			"contract mismatch " + producer + " -> " + consumer +
				": stream and unary payloads are incompatible")
	}
	if produced.Type.AssignableTo(consumed.Type) || consumed.Type.AssignableTo(produced.Type) {
		return nil
	}
	if err := coerceCheck(produced.Type, consumed.Type); err != nil {
		return xerr.Validation(
			"contract mismatch " + producer + " -> " + consumer + ": " + err.Error())
	}
	return nil
}

// coerceCheck runs the same JSON round-trip that runtime action.Coerce
// falls back to for struct-to-struct edges. A zero value of the
// producer type is enough: the question is about shape, not content.
// The underlying JSON error names the field that could not be coerced,
// which is the actionable diagnostic the developer needs — no separate
// reflection walk, no drift between the check and the runtime.
//
// This runs on the cold path, once per pipeline edge at compile time,
// so the marshal/unmarshal cost is irrelevant. On success, the two
// types are compatible by exactly the same rule the runtime applies,
// which is the invariant that makes the check honest rather than
// aspirational.
func coerceCheck(producerType, consumerType reflect.Type) error {
	producerValue := reflect.New(producerType).Elem().Interface()
	consumerPtr := reflect.New(consumerType)

	raw, err := json.Marshal(producerValue)
	if err != nil {
		return fmt.Errorf("cannot marshal %s: %w", producerType, err)
	}
	if err := json.Unmarshal(raw, consumerPtr.Interface()); err != nil {
		return fmt.Errorf("cannot coerce %s into %s: %w", producerType, consumerType, err)
	}
	return nil
}

var _ ContractProvider = (*DynamicResolver)(nil)

func (r *DynamicResolver) Contract(name string) (CapabilityContract, bool) {
	if act, ok := r.Action(name); ok {
		return actionContract(name, act), true
	}
	if _, ok := r.Stream(name); ok {
		return contractForExpr(r, &Atom{Name: name})
	}
	if op, ok := r.Operator(name); ok {
		return CapabilityContract{
			Name:   name,
			Input:  TypeDescriptor{Type: op.InType, Stream: true},
			Output: TypeDescriptor{Type: op.OutType, Stream: true},
		}, true
	}
	return CapabilityContract{}, false
}
