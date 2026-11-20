package compiler

import (
	"context"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler/secret"
)

func TestValidateSecretDeliveryRequiresExplicitSecretParameter(t *testing.T) {
	atom := &Atom{
		Name:      "http.request",
		ArgSchema: []ArgFieldSpec{{Name: "bearer", Kind: ArgSecret}},
	}
	if err := validateSecretDelivery(atom, map[string]any{"bearer": secret.New("sensitive-canary")}); err != nil {
		t.Fatalf("typed bearer was rejected: %v", err)
	}
	if err := validateSecretDelivery(atom, map[string]any{"bearer": "sensitive-canary"}); err == nil {
		t.Fatal("plain string was accepted for ArgSecret")
	}
	if err := validateSecretDelivery(atom, map[string]any{"headers": map[string]any{"Authorization": secret.New("sensitive-canary")}}); err == nil {
		t.Fatal("nested Secret was accepted in an untyped argument")
	}
	if err := validateSecretDelivery(&Atom{Name: "probe.echo"}, map[string]any{"token": secret.New("sensitive-canary")}); err == nil {
		t.Fatal("Secret was accepted by an action without an ArgSecret declaration")
	}
}

func TestBuildValueExprPassesSecretsToReceiverValidation(t *testing.T) {
	ctx := context.Background()
	program, err := (&ValueExpr{Value: &Value{Kind: ValueRef}}).Build(ctx, &BuildContext{})
	if err != nil {
		t.Fatalf("build direct value: %v", err)
	}
	value, err := action.InvokeAny(ctx, program, secret.New("sensitive-canary"))
	if err != nil {
		t.Fatalf("evaluate direct value: %v", err)
	}
	if !secret.IsValue(value) {
		t.Fatalf("direct value result = %T, want secret.Value", value)
	}
	receiver := &Atom{
		Name:              "pipeline.secret",
		SecretBoundary:    true,
		SecretRequestType: reflect.TypeFor[secret.Value](),
	}
	if err := validateSecretDelivery(receiver, value); err != nil {
		t.Fatalf("typed secret receiver rejected direct value: %v", err)
	}
	if err := validateSecretDelivery(&Atom{Name: "probe.echo"}, value); err == nil {
		t.Fatal("untyped receiver accepted the direct secret value")
	}
}

func TestAnalyzeTypedAtomValuesRequiresSecretTypeForArgSecret(t *testing.T) {
	ctx := TypeAnalysisContext{
		ArgSchemas: map[string][]ArgFieldSpec{
			"http.request": {{Name: "bearer", Kind: ArgSecret}},
		},
		CheckExpression: func(string, TypeAnalysisContext, Position) (TypeDescriptor, error) {
			return TypeDescriptor{Type: reflect.TypeFor[secret.Value]()}, nil
		},
	}
	atom := &Atom{
		Name: "http.request",
		Args: map[string]*Value{"bearer": {Kind: ValueExpression, Expression: "api_key"}},
	}
	if err := analyzeTypedAtomValues(ctx, atom); err != nil {
		t.Fatalf("typed ArgSecret expression was rejected: %v", err)
	}
	ctx.CheckExpression = func(string, TypeAnalysisContext, Position) (TypeDescriptor, error) {
		return TypeDescriptor{Type: reflect.TypeFor[string]()}, nil
	}
	if err := analyzeTypedAtomValues(ctx, atom); err == nil {
		t.Fatal("ordinary string expression was accepted for ArgSecret")
	}
}

func TestAnalyzeTypedNodeRejectsStringForSecretPipelineField(t *testing.T) {
	type request struct {
		APIKey secret.Value `json:"api_key"`
	}
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	child := action.New("pipeline.child", func(context.Context, map[string]any) (any, error) {
		return nil, nil
	}).Build()
	if err := resolver.Mount(action.Library{Name: "test", Actions: []action.AnyAction{child}}); err != nil {
		t.Fatalf("mount child action: %v", err)
	}
	contract := CapabilityContract{
		Input:          TypeDescriptor{Type: reflect.TypeFor[request]()},
		Output:         TypeDescriptor{Type: reflect.TypeFor[map[string]any]()},
		SecretBoundary: true,
	}
	if err := resolver.RegisterContract("pipeline.child", contract); err != nil {
		t.Fatalf("register child contract: %v", err)
	}
	atom := &Atom{
		Name:              "pipeline.child",
		NamedPipelineArgs: true,
		Args:              map[string]*Value{"api_key": {Kind: ValueString, Str: "sensitive-canary"}},
	}
	if _, err := AnalyzeTypedExpression(TypeAnalysisContext{Resolver: resolver}, atom); err == nil {
		t.Fatal("plain string unexpectedly fit a secret-typed pipeline request field")
	}
	atom.Args["api_key"] = &Value{Kind: ValueTyped, Typed: secret.New("sensitive-canary")}
	if _, err := AnalyzeTypedExpression(TypeAnalysisContext{Resolver: resolver}, atom); err != nil {
		t.Fatalf("typed Secret did not fit its pipeline request field: %v", err)
	}
}

func TestAnalyzeTypedAtomValuesAllowsOnlyExactSecretPipelineDirectInput(t *testing.T) {
	type request struct {
		APIKey secret.Value `json:"api_key"`
	}
	type wrongRequest struct {
		Other secret.Value `json:"other"`
	}
	ctx := TypeAnalysisContext{
		CheckExpression: func(string, TypeAnalysisContext, Position) (TypeDescriptor, error) {
			return TypeDescriptor{Type: reflect.TypeFor[request](), ContainsSecret: true}, nil
		},
	}
	atom := &Atom{
		Name:              "pipeline.child",
		SecretBoundary:    true,
		SecretRequestType: reflect.TypeFor[request](),
		DirectInput:       &Value{Kind: ValueExpression, Expression: "request"},
	}
	if err := analyzeTypedAtomValues(ctx, atom); err != nil {
		t.Fatalf("exact Secret request was rejected: %v", err)
	}
	ctx.CheckExpression = func(string, TypeAnalysisContext, Position) (TypeDescriptor, error) {
		return TypeDescriptor{Type: reflect.TypeFor[wrongRequest](), ContainsSecret: true}, nil
	}
	if err := analyzeTypedAtomValues(ctx, atom); err == nil {
		t.Fatal("mismatched Secret request was accepted")
	}
	ctx.CheckExpression = func(string, TypeAnalysisContext, Position) (TypeDescriptor, error) {
		return TypeDescriptor{Type: reflect.TypeFor[request](), ContainsSecret: true}, nil
	}
	atom.SecretBoundary = false
	if err := analyzeTypedAtomValues(ctx, atom); err == nil {
		t.Fatal("Secret request was accepted without a schema boundary")
	}
}

func TestValidateSecretDeliveryHonorsTypedPipelineBoundary(t *testing.T) {
	// The schema's source representation is opaque even though JSON sends a
	// string. A runtime action request must carry Secret.Value, not its wire form.
	type typedRequest struct {
		APIKey secret.Value `json:"api_key"`
		Note   string       `json:"note"`
	}
	atom := &Atom{
		Name:              "pipeline.child",
		SecretBoundary:    true,
		SecretRequestType: reflect.TypeFor[typedRequest](),
	}
	if err := validateSecretDelivery(atom, map[string]any{
		"api_key": secret.New("sensitive-canary"), "note": "safe",
	}); err != nil {
		t.Fatalf("typed pipeline request was rejected: %v", err)
	}
	if err := validateSecretDelivery(atom, map[string]any{
		"api_key": "sensitive-canary", "note": "safe",
	}); err == nil {
		t.Fatal("plain string unexpectedly crossed a typed Secret pipeline boundary")
	}
	if err := validateSecretDelivery(atom, map[string]any{
		"wrong": secret.New("sensitive-canary"),
	}); err == nil {
		t.Fatal("Secret in an undeclared field crossed the pipeline boundary")
	}
	atom.SecretInputNormalizer = true
	if err := validateSecretDelivery(atom, map[string]any{
		"api_key": "sensitive-canary", "note": "safe",
	}); err != nil {
		t.Fatalf("wire string was rejected before its declared schema normalizer: %v", err)
	}
}

func TestBuildPayloadDoesNotInheritSecretFields(t *testing.T) {
	payload, err := buildPayload(context.Background(), map[string]any{
		"api_key": secret.New("sensitive-canary"),
		"visible": "safe",
	}, &Atom{Name: "probe.echo"})
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}
	if _, present := payload["api_key"]; present {
		t.Fatalf("inherited secret field was forwarded: %#v", payload)
	}
	if payload["visible"] != "safe" {
		t.Fatalf("non-secret inherited field was dropped: %#v", payload)
	}
	if secret.Contains(payload) {
		t.Fatalf("constructed payload still contains a Secret: %#v", payload)
	}
}
