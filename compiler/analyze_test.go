package compiler

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
)

// Same field name, incompatible types. coerceCheck marshals the
// producer and unmarshals into the consumer; a string cannot land in
// an int field, so the mismatch is detected. Differently-named fields
// would silently succeed via encoding/json's ignore-unknown-key
// behavior and these tests would pass vacuously.
type (
	analyzeIn           struct{ ID int }
	analyzeOut          struct{ ID string }
	analyzeDynamicOut   struct{ Value any }
	analyzeDynamicIn    struct{ Value int }
	analyzeDynamicAnyIn struct{ Value any }
	analyzeTaggedOut    struct {
		Value any `json:"value"`
	}
	analyzeTaggedIn struct {
		Value int `json:"VALUE"`
	}
	analyzeExactTagOut struct {
		Value int32 `json:"foo"`
	}
	analyzeExactTagIn struct {
		Folded int64 `json:"FOO"`
		Exact  int8  `json:"foo"`
	}
	analyzePlainEmbedded  struct{ Value int }
	analyzeTaggedEmbedded struct {
		Value any `json:"Value"`
	}
	analyzeTaggedEmbeddedOut struct {
		analyzePlainEmbedded
		analyzeTaggedEmbedded
	}
	analyzeDirectOverrideOut struct {
		analyzeEmbeddedFields
		Value int
	}
	analyzeDirectOverrideIn  struct{ Value int }
	analyzeInvalidTagIn      struct{ Value int }
	analyzeJSONV2Marshaler   struct{}
	analyzeJSONV2Unmarshaler struct{}
	analyzeEmbedded          struct{ analyzeEmbeddedFields }
	analyzeEmbeddedFields    struct{ Value any }
	analyzeFloatOut          struct{ Value float64 }
	analyzeInt64Out          struct{ Value int64 }
	analyzeInt8In            struct{ Value int8 }
	analyzeInt32Out          struct{ Value int32 }
	analyzeInt64In           struct{ Value int64 }
	analyzeStringValueIn     struct{ Value string }
	analyzeOmittedInt        struct {
		Value int `json:",omitempty"`
	}
	analyzeOmittedString struct {
		Value string `json:",omitempty"`
	}
)

func (analyzeJSONV2Marshaler) MarshalJSONTo(*jsontext.Encoder) error {
	return nil
}

func (*analyzeJSONV2Unmarshaler) UnmarshalJSONFrom(*jsontext.Decoder) error {
	return nil
}

func embeddedJSONTagValue() any {
	typeOfValue := reflect.StructOf([]reflect.StructField{{
		Name: "Inner",
		Type: reflect.TypeFor[analyzeEmbeddedFields](),
		Tag:  reflect.StructTag(`json:",embed"`),
	}})
	value := reflect.New(typeOfValue).Elem()
	value.Field(0).Set(reflect.ValueOf(analyzeEmbeddedFields{Value: "not an integer"}))
	return value.Interface()
}

func malformedJSONTagValue() any {
	typeOfValue := reflect.StructOf([]reflect.StructField{{
		Name: "Actual",
		Type: reflect.TypeFor[any](),
		Tag:  reflect.StructTag(`json:"Value'Name"`),
	}})
	value := reflect.New(typeOfValue).Elem()
	value.Field(0).Set(reflect.ValueOf("not an integer"))
	return value.Interface()
}

func mustResolver(tb testing.TB, libs ...action.Library) *DynamicResolver {
	tb.Helper()
	resolver, err := NewDynamicResolver(libs...)
	ktest.RequireNoError(tb, err)
	return resolver
}

func typedAction[Req, Res any](name string) *action.BuiltAction[Req, Res] {
	return action.New(name, func(context.Context, Req) (Res, error) {
		var zero Res
		return zero, nil
	}).Build()
}

// testPipe and testParallel are the minimal operator table the analyzer
// test needs to build PipeExpr and ParallelExpr from source text. Kept
// inline because importing extensions/syntax would create an import
// cycle: extensions/syntax imports compiler.
func testPipe() Operator {
	return Operator{
		Meta: OperatorMeta{
			Name:          "pipe",
			Token:         TokArrow,
			Precedence:    7,
			Associativity: Left,
		},
		Handler: func(_ context.Context, req OperatorReq) (OperatorRes, error) {
			return OperatorRes{Node: &PipeExpr{L: req.Left, R: req.Right}}, nil
		},
	}
}

func testParallel() Operator {
	return Operator{
		Meta: OperatorMeta{
			Name:          "parallel",
			Token:         TokAmpersand,
			Precedence:    5,
			Associativity: Left,
		},
		Handler: func(_ context.Context, req OperatorReq) (OperatorRes, error) {
			return OperatorRes{
				Node: &ParallelExpr{Branches: []Expr{req.Left, req.Right}},
			}, nil
		},
	}
}

func testTable() *OperatorTable {
	return NewOperatorTable(testPipe(), testParallel())
}

func TestAnalyze_NoResolver(t *testing.T) {
	t.Parallel()
	err := Analyze(nil, &Atom{Name: "any"})
	ktest.RequireErrorContains(t, err, "capability resolver is nil")
}

func TestAnalyze_SingleAtomAlwaysPasses(t *testing.T) {
	t.Parallel()
	err := Analyze(mustResolver(t), &Atom{Name: "not-registered"})
	ktest.RequireNoError(t, err)
}

func TestAnalyze_PipeEdges(t *testing.T) {
	t.Parallel()

	producer := typedAction[any, analyzeOut]("test.producer")
	consumerGood := typedAction[analyzeOut, any]("test.consumer_good")
	consumerBad := typedAction[analyzeIn, any]("test.consumer_bad")
	resolver := mustResolver(t, action.Library{
		Name:    "test",
		Actions: []action.AnyAction{producer, consumerGood, consumerBad},
	})

	cases := []struct {
		name    string
		left    string
		right   string
		wantErr bool
		errHint string
	}{
		{"compatible types", "test.producer", "test.consumer_good", false, ""},
		{"incompatible types", "test.producer", "test.consumer_bad", true, "contract mismatch"},
		{"unknown left skipped", "missing", "test.consumer_good", false, ""},
		{"unknown right skipped", "test.producer", "missing", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ast := &PipeExpr{
				L: &Atom{Name: tc.left},
				R: &Atom{Name: tc.right},
			}
			err := Analyze(resolver, ast)
			if tc.wantErr {
				ktest.RequireErrorContains(t, err, tc.errHint)
				return
			}
			ktest.RequireNoError(t, err)
		})
	}
}

func TestAnalyze_RejectsDynamicStreamOutputForConcreteConsumer(t *testing.T) {
	t.Parallel()

	dynamic := action.NewSimpleOperator[any, any]("test.dynamic", func(up iter.Seq2[any, error]) iter.Seq2[any, error] {
		return up
	})
	typed := action.NewSimpleOperator[analyzeOut, analyzeOut]("test.typed", func(up iter.Seq2[analyzeOut, error]) iter.Seq2[analyzeOut, error] {
		return up
	})
	resolver := mustResolver(t, action.Library{
		Name:      "test",
		Operators: []action.NamedOperator{dynamic, typed},
	})

	ast := &PipeExpr{L: &Atom{Name: "test.dynamic"}, R: &Atom{Name: "test.typed"}}
	err := Analyze(resolver, ast)
	ktest.RequireErrorContains(t, err, "dynamic output")
}

func TestRequireCompatibleRejectsValueDependentStructuralCoercions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		produced reflect.Type
		consumed reflect.Type
		errHint  string
	}{
		{"interface field to concrete field", reflect.TypeFor[analyzeDynamicOut](), reflect.TypeFor[analyzeDynamicIn](), "dynamic value type"},
		{"tagged interface field", reflect.TypeFor[analyzeTaggedOut](), reflect.TypeFor[analyzeTaggedIn](), "dynamic value type"},
		{"embedded interface field", reflect.TypeFor[analyzeEmbedded](), reflect.TypeFor[analyzeDynamicIn](), "dynamic value type"},
		{"map interface value to concrete field", reflect.TypeFor[map[string]any](), reflect.TypeFor[analyzeDynamicIn](), "dynamic value type"},
		{"slice interface value to concrete element", reflect.TypeFor[[]any](), reflect.TypeFor[[]int](), "dynamic value type"},
		{"float to integer", reflect.TypeFor[analyzeFloatOut](), reflect.TypeFor[analyzeDynamicIn](), "value-dependent numeric coercion"},
		{"narrowing integer", reflect.TypeFor[analyzeInt64Out](), reflect.TypeFor[analyzeInt8In](), "value-dependent numeric coercion"},
		{"int64 to float64", reflect.TypeFor[int64](), reflect.TypeFor[float64](), "value-dependent numeric coercion"},
		{"int32 to float32", reflect.TypeFor[int32](), reflect.TypeFor[float32](), "value-dependent numeric coercion"},
		{"float32 to float64", reflect.TypeFor[float32](), reflect.TypeFor[float64](), "value-dependent numeric coercion"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := requireCompatible(TypeDescriptor{Type: tc.produced}, TypeDescriptor{Type: tc.consumed}, "producer", "consumer")
			ktest.RequireErrorContains(t, err, tc.errHint)
		})
	}
}

func TestRequireCompatibleAllowsGuaranteedStructuralCoercions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		produced reflect.Type
		consumed reflect.Type
	}{
		{"interface field to interface field", reflect.TypeFor[analyzeDynamicOut](), reflect.TypeFor[analyzeDynamicAnyIn]()},
		{"integer widening", reflect.TypeFor[analyzeInt32Out](), reflect.TypeFor[analyzeInt64In]()},
		{"int32 to float64", reflect.TypeFor[int32](), reflect.TypeFor[float64]()},
		{"uint32 to float64", reflect.TypeFor[uint32](), reflect.TypeFor[float64]()},
		{"int16 to float32", reflect.TypeFor[int16](), reflect.TypeFor[float32]()},
		{"integer map key widening", reflect.TypeFor[map[int32]int](), reflect.TypeFor[map[int64]int]()},
		{"integer map key to string", reflect.TypeFor[map[int]int](), reflect.TypeFor[map[string]int]()},
		{"string to bytes", reflect.TypeFor[string](), reflect.TypeFor[[]byte]()},
		{"bytes to string", reflect.TypeFor[[]byte](), reflect.TypeFor[string]()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := requireCompatible(TypeDescriptor{Type: tc.produced}, TypeDescriptor{Type: tc.consumed}, "producer", "consumer")
			ktest.RequireNoError(t, err)
		})
	}
}

func TestRequireCompatibleUsesJSONFieldSelection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		produced reflect.Type
		consumed reflect.Type
		errHint  string
	}{
		{"exact tag beats folded name", reflect.TypeFor[analyzeExactTagOut](), reflect.TypeFor[analyzeExactTagIn](), "value-dependent numeric coercion"},
		{"tagged embedded field dominates", reflect.TypeFor[analyzeTaggedEmbeddedOut](), reflect.TypeFor[analyzeDynamicIn](), "dynamic value type"},
		{"direct field shadows embedded field", reflect.TypeFor[analyzeDirectOverrideOut](), reflect.TypeFor[analyzeDirectOverrideIn](), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := requireCompatible(TypeDescriptor{Type: tc.produced}, TypeDescriptor{Type: tc.consumed}, "producer", "consumer")
			if tc.errHint == "" {
				ktest.RequireNoError(t, err)
				return
			}
			ktest.RequireErrorContains(t, err, tc.errHint)
		})
	}
	malformedTagErr := requireCompatible(
		TypeDescriptor{Type: reflect.TypeOf(malformedJSONTagValue())},
		TypeDescriptor{Type: reflect.TypeFor[analyzeInvalidTagIn]()},
		"producer",
		"consumer",
	)
	ktest.RequireErrorContains(t, malformedTagErr, "JSON")
}

func TestRequireCompatibleRejectsJSONEmbedTag(t *testing.T) {
	t.Parallel()

	value := embeddedJSONTagValue()
	err := requireCompatible(
		TypeDescriptor{Type: reflect.TypeOf(value)},
		TypeDescriptor{Type: reflect.TypeFor[analyzeDynamicIn]()},
		"producer",
		"consumer",
	)
	ktest.RequireErrorContains(t, err, "embed")

	_, runtimeErr := action.Coerce[analyzeDynamicIn](value)
	ktest.RequireErrorContains(t, runtimeErr, "cannot unmarshal")
}

func TestRequireCompatibleRejectsJSONV2Codecs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		produced reflect.Type
		consumed reflect.Type
	}{
		{"v2 marshaler", reflect.TypeFor[analyzeJSONV2Marshaler](), reflect.TypeFor[map[string]any]()},
		{"v2 unmarshaler", reflect.TypeFor[map[string]any](), reflect.TypeFor[analyzeJSONV2Unmarshaler]()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := requireCompatible(TypeDescriptor{Type: tc.produced}, TypeDescriptor{Type: tc.consumed}, "producer", "consumer")
			ktest.RequireErrorContains(t, err, "custom JSON codec")
		})
	}
}

func TestRequireCompatibleRejectsNullMaskedStructuralCoercions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		produced reflect.Type
		consumed reflect.Type
	}{
		{"nil sequence to struct", reflect.TypeFor[[]int](), reflect.TypeFor[analyzeInt64In]()},
		{"nil sequence to scalar", reflect.TypeFor[[]int](), reflect.TypeFor[int]()},
		{"nil map with incompatible value", reflect.TypeFor[map[string]int](), reflect.TypeFor[analyzeStringValueIn]()},
		{"nil map to scalar", reflect.TypeFor[map[string]int](), reflect.TypeFor[int]()},
		{"nil map with unsupported source key", reflect.TypeFor[map[struct{}]int](), reflect.TypeFor[analyzeStringValueIn]()},
		{"nil map with unsupported destination key", reflect.TypeFor[map[string]int](), reflect.TypeFor[map[struct{}]int]()},
		{"string map key to integer", reflect.TypeFor[map[string]int](), reflect.TypeFor[map[int]int]()},
		{"narrowing integer map key", reflect.TypeFor[map[int64]int](), reflect.TypeFor[map[int8]int]()},
		{"nil sequence with incompatible element", reflect.TypeFor[[]int](), reflect.TypeFor[[]string]()},
		{"omitted incompatible field", reflect.TypeFor[analyzeOmittedInt](), reflect.TypeFor[analyzeOmittedString]()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := requireCompatible(TypeDescriptor{Type: tc.produced}, TypeDescriptor{Type: tc.consumed}, "producer", "consumer")
			ktest.RequireErrorContains(t, err, "JSON")
		})
	}
}

func TestRuntimeCoerceCanFailForValueDependentStructuralTypes(t *testing.T) {
	t.Parallel()

	_, floatErr := action.Coerce[analyzeDynamicIn](analyzeFloatOut{Value: 1.5})
	ktest.RequireErrorContains(t, floatErr, "cannot unmarshal")

	_, integerErr := action.Coerce[analyzeInt8In](analyzeInt64Out{Value: 128})
	ktest.RequireErrorContains(t, integerErr, "cannot unmarshal")

	_, dynamicErr := action.Coerce[analyzeDynamicIn](analyzeDynamicOut{Value: "not an integer"})
	ktest.RequireErrorContains(t, dynamicErr, "cannot unmarshal")
}

func TestRuntimeCoerceFailsForNullMaskedStructuralTypes(t *testing.T) {
	t.Parallel()

	_, sequenceErr := action.Coerce[analyzeInt64In]([]int{1})
	ktest.RequireErrorContains(t, sequenceErr, "cannot unmarshal")

	_, mapErr := action.Coerce[analyzeStringValueIn](map[string]int{"Value": 1})
	ktest.RequireErrorContains(t, mapErr, "cannot unmarshal")

	_, sourceKeyErr := action.Coerce[analyzeStringValueIn](map[struct{}]int{{}: 1})
	ktest.RequireErrorContains(t, sourceKeyErr, "marshal map")

	_, destinationKeyErr := action.Coerce[map[struct{}]int](map[string]int{"Value": 1})
	ktest.RequireErrorContains(t, destinationKeyErr, "cannot unmarshal")

	_, stringKeyErr := action.Coerce[map[int]int](map[string]int{"not-an-int": 1})
	ktest.RequireErrorContains(t, stringKeyErr, "not-an-int")

	_, narrowKeyErr := action.Coerce[map[int8]int](map[int64]int{128: 1})
	ktest.RequireErrorContains(t, narrowKeyErr, "128")

	_, elementErr := action.Coerce[[]string]([]int{1})
	ktest.RequireErrorContains(t, elementErr, "cannot unmarshal")

	_, omittedFieldErr := action.Coerce[analyzeOmittedString](analyzeOmittedInt{Value: 1})
	ktest.RequireErrorContains(t, omittedFieldErr, "cannot unmarshal")
}

func TestRuntimeCoerceMatchesJSONFieldSelection(t *testing.T) {
	t.Parallel()

	_, exactTagErr := action.Coerce[analyzeExactTagIn](analyzeExactTagOut{Value: 128})
	ktest.RequireErrorContains(t, exactTagErr, "cannot unmarshal")

	_, embeddedErr := action.Coerce[analyzeDynamicIn](analyzeTaggedEmbeddedOut{
		analyzePlainEmbedded:  analyzePlainEmbedded{Value: 1},
		analyzeTaggedEmbedded: analyzeTaggedEmbedded{Value: "not an integer"},
	})
	ktest.RequireErrorContains(t, embeddedErr, "cannot unmarshal")

	_, invalidTagErr := action.Coerce[analyzeInvalidTagIn](malformedJSONTagValue())
	ktest.RequireErrorContains(t, invalidTagErr, "cannot unmarshal")

	got, overrideErr := action.Coerce[analyzeDirectOverrideIn](analyzeDirectOverrideOut{
		analyzeEmbeddedFields: analyzeEmbeddedFields{Value: "ignored"},
		Value:                 7,
	})
	ktest.RequireNoError(t, overrideErr)
	if got.Value != 7 {
		t.Fatalf("direct field value = %d, want 7", got.Value)
	}
}

func TestAnalyze_ParallelChecksEachChild(t *testing.T) {
	t.Parallel()

	producer := typedAction[any, analyzeOut]("test.producer")
	bad := typedAction[analyzeIn, any]("test.bad")
	resolver := mustResolver(t, action.Library{
		Name:    "test",
		Actions: []action.AnyAction{producer, bad},
	})

	ast := &ParallelExpr{
		Branches: []Expr{
			&PipeExpr{L: &Atom{Name: "test.producer"}, R: &Atom{Name: "test.bad"}},
			&Atom{Name: "test.producer"},
		},
	}

	err := Analyze(resolver, ast)
	ktest.RequireErrorContains(t, err, "contract mismatch")
}

func TestAnalyze_FallbackChecksBothArms(t *testing.T) {
	t.Parallel()

	producer := typedAction[any, analyzeOut]("test.producer")
	bad := typedAction[analyzeIn, any]("test.bad")
	resolver := mustResolver(t, action.Library{
		Name:    "test",
		Actions: []action.AnyAction{producer, bad},
	})

	ast := &FallbackExpr{
		L: &Atom{Name: "test.producer"},
		R: &PipeExpr{L: &Atom{Name: "test.producer"}, R: &Atom{Name: "test.bad"}},
	}

	err := Analyze(resolver, ast)
	ktest.RequireErrorContains(t, err, "contract mismatch")
}
