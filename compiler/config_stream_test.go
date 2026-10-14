package compiler

import (
	"context"
	"fmt"
	"iter"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/action"
)

type streamSourceConfig struct {
	Prefix string `json:"prefix"`
}

type streamOperatorConfig struct {
	Suffix string `json:"suffix"`
}

type configuredSuffixOperator struct {
	suffix string
}

func (o configuredSuffixOperator) Name() string { return "test.suffix" }

func (o configuredSuffixOperator) Apply(up action.AnyStream) (action.AnyStream, error) {
	return func(yield func(any, error) bool) {
		for value, err := range up {
			if err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}
			text, ok := value.(string)
			if !ok {
				if !yield(nil, fmt.Errorf("unexpected stream value type %T", value)) {
					return
				}
				return
			}
			if !yield(text+o.suffix, nil) {
				return
			}
		}
	}, nil
}

func TestConfigInjectionIntoStreamSourceAndOperator(t *testing.T) {
	t.Parallel()

	source := action.NewStream("test.items", func(_ context.Context, cfg streamSourceConfig) (iter.Seq2[any, error], error) {
		return func(yield func(any, error) bool) {
			yield(cfg.Prefix+"item", nil)
		}, nil
	})
	operator := action.NamedOperator{
		Name:       "test.suffix",
		InType:     reflect.TypeFor[any](),
		OutType:    reflect.TypeFor[any](),
		ConfigType: reflect.TypeFor[streamOperatorConfig](),
		Build: func(params any) (action.StreamOperator, error) {
			var cfg streamOperatorConfig
			if err := action.Assign(&cfg, params); err != nil {
				return nil, err
			}
			return configuredSuffixOperator{suffix: cfg.Suffix}, nil
		},
	}
	resolver, err := NewDynamicResolver(action.Library{
		Name:      "test",
		Sources:   []action.AnyStreamAction{source},
		Operators: []action.NamedOperator{operator},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := WithCompileConfig(context.Background(), map[string]string{
		"prefix": "configured-",
		"suffix": "-configured",
	}, nil)
	ast, err := NewParserWithFileOffset(
		ctx,
		testTable(),
		DefaultPrimaryExtensions(),
		"test.items @config -> test.suffix @config",
		"",
		0,
	).Parse()
	if err != nil {
		t.Fatal(err)
	}
	program, err := Build(ctx, resolver, nil, ast)
	if err != nil {
		t.Fatal(err)
	}

	output, err := action.InvokeAny(ctx, program, nil)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := output.([]any)
	if !ok {
		t.Fatalf("output is %T, want []any", output)
	}
	if len(items) != 1 || items[0] != "configured-item-configured" {
		t.Fatalf("items = %#v, want one configured item", items)
	}
}

type typedSourceRequest struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix,omitempty"`
}

func TestConfiguredSourceMergesTypedRequestAndConfig(t *testing.T) {
	t.Parallel()

	source := action.NewStream("test.typed", func(_ context.Context, req typedSourceRequest) (iter.Seq2[any, error], error) {
		return func(yield func(any, error) bool) {
			yield(req.Name+":"+req.Prefix, nil)
		}, nil
	})
	configured := &configuredSource{
		inner: source,
		cfg:   map[string]any{"prefix": "configured"},
	}

	tests := []struct {
		name string
		req  typedSourceRequest
		want string
	}{
		{name: "explicit request wins", req: typedSourceRequest{Name: "repo", Prefix: "request"}, want: "repo:request"},
		{name: "omitted request field uses config", req: typedSourceRequest{Name: "repo"}, want: "repo:configured"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stream, err := configured.DoStreamAny(context.Background(), test.req)
			if err != nil {
				t.Fatal(err)
			}
			var (
				got       []any
				streamErr error
			)
			stream(func(item any, err error) bool {
				if err != nil {
					streamErr = err
					return false
				}
				got = append(got, item)
				return true
			})
			if streamErr != nil {
				t.Fatal(streamErr)
			}
			if len(got) != 1 || got[0] != test.want {
				t.Fatalf("items = %#v, want [%q]", got, test.want)
			}
		})
	}
}

func TestConfiguredSourceRejectsNonObjectRequest(t *testing.T) {
	t.Parallel()

	source := action.NewStream("test.typed", func(_ context.Context, _ typedSourceRequest) (iter.Seq2[any, error], error) {
		return func(yield func(any, error) bool) { yield("unused", nil) }, nil
	})
	configured := &configuredSource{inner: source, cfg: map[string]any{"prefix": "configured"}}
	if _, err := configured.DoStreamAny(context.Background(), "scalar request"); err == nil {
		t.Fatal("expected configured source to reject a non-object request")
	}
}
