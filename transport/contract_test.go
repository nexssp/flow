package transport_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexssp/flow/dslparse"
	flowtransport "github.com/nexssp/flow/transport"
	"github.com/nexssp/kernel/action"
)

type testConfig struct {
	Address string        `flow:"address" default:"127.0.0.1:4222"`
	Timeout time.Duration `flow:"timeout" default:"2s"`
}

type testBinding struct{ kind string }

type testStream struct{}

func (testStream) Describe() *action.Meta        { return &action.Meta{Name: "contract.stream"} }
func (testStream) ReqPayload() any               { return "" }
func (testStream) ResPayload() any               { return "" }
func (testStream) GetBindings() []action.Binding { return nil }
func (testStream) GetAnyHooks() []action.AnyHook { return nil }
func (testStream) AddAnyHook(...action.AnyHook)  {}
func (testStream) DoStreamAny(context.Context, any) (action.AnyStream, error) {
	return nil, errors.New("not executable in contract test")
}
func (testStream) CloneWithHooks(...action.AnyHook) action.AnyStreamAction { return testStream{} }

func TestContract_LibraryRegisterLoadAndDecode(t *testing.T) {
	const prefix = "contract.test.library"
	flowtransport.Register(prefix, func(_ context.Context, cfg testConfig) (action.Library, error) {
		return action.Library{Name: cfg.Address}, nil
	})

	lib, err := flowtransport.Load(context.Background(), prefix, map[string]string{"timeout": "3s"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if lib.Name != "127.0.0.1:4222" {
		t.Fatalf("default config not applied: %q", lib.Name)
	}

	if _, err := flowtransport.Load(context.Background(), prefix, map[string]string{"unknown": "value"}); err == nil {
		t.Fatal("unknown option was accepted")
	}

	known := flowtransport.KnownPrefixes()
	for i, name := range known {
		if name == prefix {
			if i > 0 && known[i-1] > name {
				t.Fatal("KnownPrefixes is not sorted")
			}
			return
		}
	}
	t.Fatalf("registered prefix %q not listed", prefix)
}

func TestContract_RegisterRejectsDuplicate(t *testing.T) {
	const prefix = "contract.test.duplicate"
	flowtransport.Register(prefix, func(context.Context, struct{}) (action.Library, error) {
		return action.Library{Name: prefix}, nil
	})

	deferred := false
	func() {
		defer func() {
			deferred = recover() != nil
		}()
		flowtransport.Register(prefix, func(context.Context, struct{}) (action.Library, error) {
			return action.Library{}, nil
		})
	}()
	if !deferred {
		t.Fatal("duplicate registration did not panic")
	}
}

func TestContract_ResolveModifierThroughActionRegistry(t *testing.T) {
	resolver := action.New("contract.test.resolver", func(_ context.Context, binding *dslparse.TransportBinding) (action.Binding, error) {
		return testBinding{kind: binding.Kind + "=" + binding.Target}, nil
	}).Route(flowtransport.OnDSL("http")).Build()
	reg, err := action.NewRegistry(action.Library{Name: "contract", Actions: []action.AnyAction{resolver}})
	if err != nil {
		t.Fatal(err)
	}

	binding, handled, err := flowtransport.ResolveModifier(context.Background(), reg, &dslparse.TransportBinding{Kind: "http", Target: "/health"})
	if err != nil || !handled {
		t.Fatalf("ResolveModifier: handled=%v err=%v", handled, err)
	}
	if got := binding.(testBinding).kind; got != "http=/health" {
		t.Fatalf("binding: got %q", got)
	}
}

func TestContract_FindTriggerAndRequireTrigger(t *testing.T) {
	trigger := action.New("contract.test.trigger", func(context.Context, any) (any, error) {
		return nil, nil
	}).Route(flowtransport.OnTrigger("nats")).Build()
	lib := action.Library{Name: "contract", Actions: []action.AnyAction{trigger}}

	got, ok := flowtransport.FindTrigger(lib, "nats")
	if !ok || got != trigger {
		t.Fatal("FindTrigger did not return the registered trigger")
	}
	if _, err := flowtransport.RequireTrigger(lib, "http"); err == nil {
		t.Fatal("RequireTrigger accepted an unknown protocol")
	}
}

func TestContract_AsLibraryPreservesActionAndStreamKinds(t *testing.T) {
	actionValue := action.New("contract.test.action", func(context.Context, string) (string, error) {
		return "", nil
	}).Build()

	if got := flowtransport.AsLibrary(actionValue); len(got.Actions) != 1 || got.Actions[0] != actionValue {
		t.Fatal("action was not normalized into Library.Actions")
	}
	if got := flowtransport.AsLibrary(testStream{}); len(got.Sources) != 1 {
		t.Fatal("stream was not normalized into Library.Sources")
	}
	if got := flowtransport.AsLibrary(struct{}{}); len(got.Actions) != 0 || len(got.Sources) != 0 {
		t.Fatal("unknown workload was not normalized to an empty library")
	}
}
