package flow_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/testkit"
)

type userPayload struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type projectPayload struct {
	ID      string `json:"id"`
	OwnerID string `json:"owner_id"`
	Name    string `json:"name"`
}

func TestAssembler_DSLGeneratesRoutesAndPipelines(t *testing.T) {
	t.Parallel()

	userCreate := action.New("users.create", func(_ context.Context, u userPayload) (userPayload, error) {
		u.ID = "usr_42"

		return u, nil
	}).Build()

	projectCreate := action.New("projects.create", func(_ context.Context, p projectPayload) (projectPayload, error) {
		p.ID = "prj_101"

		return p, nil
	}).Build()

	assembler := flow.NewAssembler(userCreate, projectCreate)

	manifest := `
	users.create:route="POST /api/users":status=201
	users.create -> { owner_id: id, name: "AutoProj" } -> projects.create:route="POST /api/onboard":status=201
	`

	actions, err := assembler.AssembleManifest(manifest)
	if err != nil {
		t.Fatalf("AssembleManifest failed: %v", err)
	}

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions compiled, got %d", len(actions))
	}

	suite := testkit.New(t, actions)

	// 1. Direct route created by DSL
	suite.POST("/api/users", userPayload{Name: "Ada"}).
		Do().
		ExpectCreated().
		HasField("id", "usr_42")

	// 2. Composite pipeline route created by DSL
	suite.POST("/api/onboard", userPayload{Name: "Bob"}).
		Do().
		ExpectCreated().
		HasField("id", "prj_101").
		HasField("owner_id", "usr_42").
		HasField("name", "AutoProj")
}
