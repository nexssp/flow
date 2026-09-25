package flow_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/dslparse"
	flowtransport "github.com/nexssp/flow/transport"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
)

// Lokalny mock bindingu tras dla testu assemblera
type testRoute struct {
	Method string
	Path   string
}

func (r testRoute) HTTPRoute() (string, string) { return r.Method, r.Path }

func TestAssembler_DSLGeneratesRoutesAndPipelines(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockResolver := action.New("http.mock_resolver", func(_ context.Context, b *dslparse.TransportBinding) (action.Binding, error) {
		return testRoute{Method: b.Method, Path: b.Path}, nil
	}).Route(flowtransport.OnDSL("http"), flowtransport.OnDSL("route")).Build()

	userCreate := action.New("users.create", func(_ context.Context, u userPayload) (userPayload, error) {
		u.ID = "usr_42"
		return u, nil
	}).Build()

	projectCreate := action.New("projects.create", func(_ context.Context, p projectPayload) (projectPayload, error) {
		p.ID = "prj_101"
		return p, nil
	}).Build()

	assembler := flow.NewAssembler(userCreate, projectCreate, mockResolver)

	manifest := `
	users.create:route="POST /api/users":status=201
	users.create -> { owner_id: id, name: "AutoProj" } -> projects.create:route="POST /api/onboard":status=201
	`

	actions, err := assembler.AssembleManifest(manifest)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(actions), 2)

	// 1. Sprawdzenie pierwszej akcji (users.create)
	res1, err := actions[0].DoAny(ctx, userPayload{Name: "Ada"})
	ktest.RequireNoError(t, err)
	u1, ok := res1.(userPayload)
	ktest.RequireCondition(t, ok, "oczekiwano userPayload")
	ktest.RequireEqual(t, u1.ID, "usr_42")

	// 2. Sprawdzenie złożonego pipeline'u (users.create -> projection -> projects.create)
	res2, err := actions[1].DoAny(ctx, userPayload{Name: "Bob"})
	ktest.RequireNoError(t, err)
	p2, ok := res2.(projectPayload)
	ktest.RequireCondition(t, ok, "oczekiwano projectPayload")
	ktest.RequireEqual(t, p2.ID, "prj_101")
	ktest.RequireEqual(t, p2.OwnerID, "usr_42")
	ktest.RequireEqual(t, p2.Name, "AutoProj")
}

type userPayload struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type projectPayload struct {
	ID      string `json:"id"`
	OwnerID string `json:"owner_id"`
	Name    string `json:"name"`
}
