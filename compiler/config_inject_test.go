package compiler

import (
	"context"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
)

// testConfig covers every kind coerceConfigValue handles plus two
// fields that must be skipped: json:"-" and an unexported field.
type testConfig struct {
	Name    string   `json:"name"`
	Active  bool     `json:"active"`
	Count   int      `json:"count"`
	UCount  uint     `json:"ucount"`
	Ratio   float64  `json:"ratio"`
	Tags    []string `json:"tags"`
	Skipped string   `json:"-"`
	hidden  string   //nolint:unused // exercised via reflection; test asserts unexported fields are skipped
}

func TestInjectConfigIntoParams_HappyPath(t *testing.T) {
	t.Parallel()

	cfg := map[string]string{
		"name":   "Maksio",
		"active": "true",
		"count":  "42",
		"ucount": "7",
		"ratio":  "3.14",
		"tags":   "go,ts,rust",
	}

	params, err := injectConfigIntoParams(map[string]any{}, testConfig{}, cfg)
	ktest.RequireNoError(t, err)

	name, ok := params["name"].(string)
	ktest.RequireCondition(t, ok, "name is %T, want string", params["name"])
	ktest.RequireEqual(t, name, "Maksio")

	active, ok := params["active"].(bool)
	ktest.RequireCondition(t, ok, "active is %T, want bool", params["active"])
	ktest.RequireEqual(t, active, true)

	count, ok := params["count"].(int)
	ktest.RequireCondition(t, ok, "count is %T, want int", params["count"])
	ktest.RequireEqual(t, count, 42)

	ucount, ok := params["ucount"].(uint)
	ktest.RequireCondition(t, ok, "ucount is %T, want uint", params["ucount"])
	ktest.RequireEqual(t, ucount, uint(7))

	ratio, ok := params["ratio"].(float64)
	ktest.RequireCondition(t, ok, "ratio is %T, want float64", params["ratio"])
	ktest.RequireEqual(t, ratio, 3.14)

	tags, ok := params["tags"].([]string)
	ktest.RequireCondition(t, ok, "tags is %T, want []string", params["tags"])
	ktest.RequireEqual(t, tags, []string{"go", "ts", "rust"})
}

func TestInjectConfigIntoParams_ReflectTypeTarget(t *testing.T) {
	t.Parallel()

	cfg := map[string]string{"name": "Maksymilian", "active": "true"}
	params, err := injectConfigIntoParams(nil, reflect.TypeFor[testConfig](), cfg)
	ktest.RequireNoError(t, err)

	name, ok := params["name"].(string)
	ktest.RequireCondition(t, ok, "name is %T, want string", params["name"])
	ktest.RequireEqual(t, name, "Maksymilian")
	active, ok := params["active"].(bool)
	ktest.RequireCondition(t, ok, "active is %T, want bool", params["active"])
	ktest.RequireEqual(t, active, true)
}

func TestInjectConfigIntoParams_ExplicitParamsWin(t *testing.T) {
	t.Parallel()

	cfg := map[string]string{"name": "fromconfig"}

	params, err := injectConfigIntoParams(map[string]any{"name": "explicit"}, testConfig{}, cfg)
	ktest.RequireNoError(t, err)

	name, ok := params["name"].(string)
	ktest.RequireCondition(t, ok, "name is %T, want string", params["name"])
	ktest.RequireEqual(t, name, "explicit")
}

func TestInjectConfigIntoParams_SkipsNonConfigurableFields(t *testing.T) {
	t.Parallel()

	cfg := map[string]string{
		"Skipped": "should-not-appear",
		"hidden":  "should-not-appear",
	}

	params, err := injectConfigIntoParams(map[string]any{}, testConfig{}, cfg)
	ktest.RequireNoError(t, err)

	_, hasSkipped := params["Skipped"]
	_, hasHidden := params["hidden"]
	ktest.RequireCondition(t, !hasSkipped, "json:\"-\" field leaked into params")
	ktest.RequireCondition(t, !hasHidden, "unexported field leaked into params")
}

func TestInjectConfigIntoParams_NilAndNonStructTargets(t *testing.T) {
	t.Parallel()

	cfg := map[string]string{"x": "1"}

	params, err := injectConfigIntoParams(map[string]any{}, nil, cfg)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(params), 0)

	params, err = injectConfigIntoParams(map[string]any{}, "not-struct", cfg)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(params), 0)

	params, err = injectConfigIntoParams(map[string]any{}, testConfig{}, nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(params), 0)
}

func TestInjectConfigIntoParams_BadValueReportsField(t *testing.T) {
	t.Parallel()

	cfg := map[string]string{"active": "notabool"}
	_, err := injectConfigIntoParams(map[string]any{}, testConfig{}, cfg)
	ktest.RequireErrorContains(t, err, `field "active"`)
}

func TestCoerceConfigValue_PointerDereferences(t *testing.T) {
	t.Parallel()

	var boolPtr *bool
	got, err := action.CoerceStringValue("true", reflect.TypeOf(boolPtr))
	ktest.RequireNoError(t, err)

	value, ok := got.(*bool)
	ktest.RequireCondition(t, ok, "got is %T, want *bool", got)
	ktest.RequireNotNil(t, value)
	ktest.RequireEqual(t, *value, true)
}

func TestResolveConfigRefsInModifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     map[string]string
		cliArgs []string
		raw     string
		want    string
	}{
		{
			name: "config reference rewritten",
			cfg:  map[string]string{"output": "x.md"},
			raw:  "path=@config.output",
			want: "path=x.md",
		},
		{
			name: "plain value unchanged",
			raw:  "path=plain.md",
			want: "path=plain.md",
		},
		{
			name:    "flag reference rewritten",
			cliArgs: []string{"--editor=zed"},
			raw:     "editor=@flag.editor",
			want:    "editor=zed",
		},
		{
			name: "bare modifier unchanged",
			raw:  "quiet",
			want: "quiet",
		},
		{
			name: "missing config value resolves empty",
			raw:  "path=@config.missing",
			want: "path=",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, resolveConfigRefsInModifier(test.cfg, test.cliArgs, test.raw), test.want)
		})
	}
}

func TestResolveConfigRefsInUnquotedValues(t *testing.T) {
	t.Parallel()

	p := NewParser(context.Background(), NewOperatorTable(), `noop @{ config: @config.retries, flag: @flag.editor }`)
	ast, err := p.Parse()
	ktest.RequireNoError(t, err)
	atom, ok := ast.(*Atom)
	ktest.RequireCondition(t, ok, "expected *Atom, got %T", ast)

	ctx := WithCompileConfig(context.Background(), map[string]string{"retries": "3"}, []string{"--editor=zed"})
	resolveConfigRefsInAtom(ctx, atom)
	for key, want := range map[string]string{"config": "3", "flag": "zed"} {
		value := atom.Args[key]
		ktest.RequireCondition(t, value != nil && value.Kind == ValueString, "%s = %+v, want string", key, value)
		ktest.RequireEqual(t, value.Str, want)
	}
}

func TestValidateConfigRefsInUnquotedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		cfg  map[string]string
		args []string
		want string
	}{
		{
			name: "unknown config reference",
			src:  `noop @{ value: @config.missing }`,
			cfg:  map[string]string{"known": "value"},
			want: "unknown @config.missing",
		},
		{
			name: "unknown flag reference",
			src:  `noop @{ value: @flag.missing }`,
			args: []string{"--known=value"},
			want: "unknown @flag.missing",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ast, err := NewParser(context.Background(), NewOperatorTable(), test.src).Parse()
			ktest.RequireNoError(t, err)
			ktest.RequireErrorContains(t, validateConfigRefs(ast, test.cfg, test.args), test.want)
		})
	}
}

func TestParser_ConfigMarker(t *testing.T) {
	t.Parallel()

	p := NewParser(context.Background(), NewOperatorTable(), "noop @config")
	ast, err := p.Parse()
	ktest.RequireNoError(t, err)

	atom, ok := ast.(*Atom)
	ktest.RequireCondition(t, ok, "expected *Atom, got %T", ast)
	ktest.RequireCondition(t, atom.ConfigInject, "@config must set ConfigInject")
	ktest.RequireEqual(t, atom.Prompt, "")
}

func TestParser_ConfigMarkerBeforePipelineOperator(t *testing.T) {
	t.Parallel()

	ast, err := NewParserWithFileOffset(
		context.Background(), testTable(), DefaultPrimaryExtensions(),
		"test.source @config -> test.operator", "", 0,
	).Parse()
	ktest.RequireNoError(t, err)
	pipe, ok := ast.(*PipeExpr)
	ktest.RequireCondition(t, ok, "expected *PipeExpr, got %T", ast)
	source, ok := pipe.L.(*Atom)
	ktest.RequireCondition(t, ok, "expected source *Atom, got %T", pipe.L)
	ktest.RequireCondition(t, source.ConfigInject, "source @config marker must be recognized before ->")
	ktest.RequireEqual(t, source.Prompt, "")
}

func TestBuild_ConfigMarkerOnUnaryActionIsRejected(t *testing.T) {
	t.Parallel()

	noop := action.New("test.noop", func(context.Context, any) (any, error) { return nil, nil }).Build()
	resolver, err := NewDynamicResolver(action.Library{Name: "test", Actions: []action.AnyAction{noop}})
	ktest.RequireNoError(t, err)

	ctx := WithCompileConfig(context.Background(), map[string]string{"value": "configured"}, nil)
	ast, err := NewParser(ctx, NewOperatorTable(), "test.noop @config").Parse()
	ktest.RequireNoError(t, err)
	_, err = Build(ctx, resolver, nil, ast)
	ktest.RequireErrorContains(t, err, "supported only for stream sources and operators")
}

func TestParser_PromptIsNotConfigMarker(t *testing.T) {
	t.Parallel()

	p := NewParser(context.Background(), NewOperatorTable(), "noop @review")
	ast, err := p.Parse()
	ktest.RequireNoError(t, err)

	atom, ok := ast.(*Atom)
	ktest.RequireCondition(t, ok, "expected *Atom, got %T", ast)
	ktest.RequireCondition(t, !atom.ConfigInject, "@review must not set ConfigInject")
	ktest.RequireEqual(t, atom.Prompt, "review")
}

func TestInjectConfigIntoParams_AllocatesFromNil(t *testing.T) {
	t.Parallel()

	cfg := map[string]string{"name": "Maksymilian"}

	params, err := injectConfigIntoParams(nil, testConfig{}, cfg)
	ktest.RequireNoError(t, err)

	name, ok := params["name"].(string)
	ktest.RequireCondition(t, ok, "name is %T, want string", params["name"])
	ktest.RequireEqual(t, name, "Maksymilian")
}
