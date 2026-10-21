// Package approval provides declarative `@approval` gates. A gate suspends
// execution with Kernel's DAG suspension sentinel until xctx carries a token
// covering the gate name.
package approval

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

const ID = "approval"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

// Bundle installs approval declarations and their materialized gate actions.
func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:          ID,
		Libraries:   []action.Library{{Name: ID}},
		Directives:  []compiler.Directive{Directive},
		Materialize: materialize,
		Fixtures:    fixturesFS,
	}
}

func materialize(req compiler.MaterializeReq) error {
	for _, declaration := range DeclarationsFromMeta(req.Meta) {
		gate := newGateAction(declaration)
		if err := req.Resolver.Mount(action.Library{
			Name:    compiler.DeclarationOwner(req.Meta, "approval", declaration.Name, "approval."+declaration.Name),
			Actions: []action.AnyAction{gate},
		}); err != nil {
			return xerr.Internal("materialize approval gate "+declaration.Name, err)
		}
	}
	return nil
}

func newGateAction(declaration Declaration) action.AnyAction {
	return action.New("approval."+declaration.Name, func(ctx context.Context, input any) (any, error) {
		if TokenCovers(xctx.ApprovalTokenFrom(ctx), declaration.Name) {
			return input, nil
		}
		return nil, dag.Suspend(declaration.Reason, map[string]any{
			"gate":    declaration.Name,
			"role":    declaration.Role,
			"reason":  declaration.Reason,
			"expires": declaration.Expires.String(),
		})
	}).Description(fmt.Sprintf("Approval gate %q (role=%s)", declaration.Name, declaration.Role)).
		Tag("flow", "approval", "gate").Build()
}

// TokenCovers reports whether a Kernel approval token grants the named gate.
// The token grammar is `all` or a comma-separated list of case-sensitive gate
// names; whitespace around each name is ignored.
func TokenCovers(token, gate string) bool {
	if strings.TrimSpace(token) == "all" {
		return true
	}
	for candidate := range strings.SplitSeq(token, ",") {
		if strings.TrimSpace(candidate) == gate {
			return true
		}
	}
	return false
}
