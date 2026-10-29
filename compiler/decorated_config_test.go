package compiler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestDecoratedExprResolvesConfigReferencesBeforeApplyingModifiers(t *testing.T) {
	t.Parallel()

	modifiers := NewModifierTable(WithOwner(
		Duration("timeout", (*action.Builder[any, any]).Timeout), OwnerKernel,
	))
	tests := []struct {
		name    string
		config  map[string]string
		cliArgs []string
		value   string
		want    time.Duration
	}{
		{
			name:   "config reference",
			config: map[string]string{"action_timeout": "60s"},
			value:  "@config.action_timeout",
			want:   60 * time.Second,
		},
		{
			name:  "config fallback",
			value: "@config.optional_timeout??15s",
			want:  15 * time.Second,
		},
		{
			name:    "command-line flag reference",
			cliArgs: []string{"--action_timeout=45s"},
			value:   "@flag.action_timeout",
			want:    45 * time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := WithCompileConfig(context.Background(), test.config, test.cliArgs)
			decorated := &DecoratedExpr{
				Inner:     &configModifierExpr{},
				Modifiers: []string{"timeout=" + test.value},
			}
			built, err := decorated.Build(ctx, &BuildContext{Modifiers: modifiers})
			ktest.RequireNoError(t, err)

			metaProvider, ok := built.(interface{ Describe() *action.Meta })
			ktest.RequireCondition(t, ok, "built action %T does not expose metadata", built)
			ktest.RequireEqual(t, metaProvider.Describe().Timeout, test.want)
		})
	}
}

func TestDecoratedExprPassesResolvedInvalidTimeoutToKernelValidation(t *testing.T) {
	t.Parallel()

	modifiers := NewModifierTable(WithOwner(
		Duration("timeout", (*action.Builder[any, any]).Timeout), OwnerKernel,
	))
	ctx := WithCompileConfig(context.Background(), map[string]string{
		"action_timeout": "not-a-duration",
	}, nil)
	decorated := &DecoratedExpr{
		Inner:     &configModifierExpr{},
		Modifiers: []string{"timeout=@config.action_timeout"},
	}
	_, err := decorated.Build(ctx, &BuildContext{Modifiers: modifiers})
	ktest.RequireErrorContains(t, err, "expected duration")
	if strings.Contains(err.Error(), "@config.action_timeout") {
		t.Fatalf("timeout reference reached Kernel unresolved: %v", err)
	}
}

func TestConfigReferenceValidationIncludesDecoratedModifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		modifier string
		config   map[string]string
		cliArgs  []string
		wantErr  string
	}{
		{
			name:     "unknown config key",
			modifier: "timeout=@config.missing",
			config:   map[string]string{"action_timeout": "60s"},
			wantErr:  "unknown @config.missing",
		},
		{
			name:     "unknown flag",
			modifier: "timeout=@flag.missing",
			cliArgs:  []string{"--timeout=5s"},
			wantErr:  "unknown @flag.missing",
		},
		{
			name:     "missing positional argument",
			modifier: "timeout=@arg.2",
			cliArgs:  []string{"one"},
			wantErr:  "missing required positional argument 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decorated := &DecoratedExpr{
				Inner:     &LiteralExpr{Val: "value"},
				Modifiers: []string{test.modifier},
				Pos:       Position{File: "decorated.nflow", Line: 3},
			}
			err := validateConfigRefs(decorated, test.config, test.cliArgs)
			ktest.RequireErrorContains(t, err, test.wantErr)
		})
	}
}

type configModifierExpr struct{}

func (*configModifierExpr) Node()                            {}
func (*configModifierExpr) Children() []Expr                 { return nil }
func (*configModifierExpr) Analyze(CapabilityResolver) error { return nil }
func (*configModifierExpr) Build(context.Context, *BuildContext) (action.AnyAction, error) {
	return action.New("config_modifier_test", func(_ context.Context, input any) (any, error) {
		return input, nil
	}).Build(), nil
}
