package nodes

import (
	"context"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"golang.org/x/sync/errgroup"
)

const (
	distributeDefaultConcurrency = 4
	distributeMaxConcurrency     = 256
	distributeMaxItems           = 10_000
)

type DistributeMapReq struct {
	Action      string `json:"action"      validate:"required" usage:"Node name to invoke for each item"`
	Concurrency int    `json:"concurrency,omitempty"           usage:"Max in-flight invocations (default 4, max 256)"`
	Items       []any  `json:"items"       validate:"required" usage:"Items to fan out. Passed unchanged to Action."`
}

type DistributeItem struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type DistributeMapRes struct {
	Items     []DistributeItem `json:"items"`
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
}

// NewDistributeMapAction invokes another action once per item, bounded
// by concurrency. The target action is resolved against the registry
// placed in the execution context by the flow compiler.
func NewDistributeMapAction() action.AnyAction {
	return action.New("distribute.map", func(ctx context.Context, req DistributeMapReq) (DistributeMapRes, error) {
		reg := contracts.RegistryFromContext(ctx)
		if reg == nil {
			return DistributeMapRes{}, xerr.Internal("distribute.map: no registry in context")
		}

		return runDistribute(ctx, reg, req)
	}).
		Description("Invoke an action once per item, bounded by concurrency, preserving order").
		Tag("distribute", "fanout").
		Build()
}

func runDistribute(ctx context.Context, reg contracts.Registry, req DistributeMapReq) (DistributeMapRes, error) {
	if reg == nil {
		return DistributeMapRes{}, xerr.Internal("distribute.map: registry is nil")
	}

	if req.Action == "" {
		return DistributeMapRes{}, xerr.BadRequest("distribute.map: action is required")
	}

	if len(req.Items) == 0 {
		return DistributeMapRes{}, nil
	}

	if len(req.Items) > distributeMaxItems {
		return DistributeMapRes{}, xerr.BadRequest("distribute.map: items exceeds limit")
	}

	conc := req.Concurrency
	if conc <= 0 {
		conc = distributeDefaultConcurrency
	}

	if conc > distributeMaxConcurrency {
		conc = distributeMaxConcurrency
	}

	target, ok := reg.Get(req.Action)
	if !ok {
		return DistributeMapRes{}, xerr.NotFound("distribute.map: action not found: " + req.Action)
	}

	items := make([]DistributeItem, len(req.Items))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(conc)

	for i := range req.Items {
		i := i
		item := req.Items[i]

		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				items[i] = DistributeItem{Error: err.Error()}

				return nil
			}

			out, err := invokeSafely(gctx, target, item)
			if err != nil {
				items[i] = DistributeItem{Error: err.Error()}

				return nil
			}

			items[i] = DistributeItem{OK: true, Result: out}

			return nil
		})
	}

	_ = g.Wait()

	res := DistributeMapRes{Items: items, Succeeded: 0, Failed: 0}
	for i := range items {
		if items[i].OK {
			res.Succeeded++
		} else {
			res.Failed++
		}
	}

	return res, nil
}

func invokeSafely(ctx context.Context, act action.AnyAction, in any) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			out = nil
			err = xerr.PanicRecovery(r)
		}
	}()

	return action.InvokeAny(ctx, act, in)
}

type DistributeReduceReq struct {
	Strategy string           `json:"strategy" validate:"required,oneof=collect all_pass any_pass first_success count"`
	Items    []DistributeItem `json:"items"    validate:"required"`
}

type DistributeReduceRes struct {
	Strategy  string `json:"strategy"`
	AllPass   bool   `json:"all_pass,omitempty"`
	AnyPass   bool   `json:"any_pass,omitempty"`
	Succeeded int    `json:"succeeded,omitempty"`
	Failed    int    `json:"failed,omitempty"`
	Result    any    `json:"result,omitempty"`
	Results   []any  `json:"results,omitempty"`
}

func NewDistributeReduceAction() action.AnyAction {
	return action.New("distribute.reduce",
		func(_ context.Context, req DistributeReduceReq) (DistributeReduceRes, error) {
			return runReduce(req)
		}).
		Description("Fold the output of distribute.map into a single value").
		Tag("distribute", "fold").
		Build()
}

func runReduce(req DistributeReduceReq) (DistributeReduceRes, error) {
	res := DistributeReduceRes{Strategy: req.Strategy}
	if len(req.Items) == 0 {
		switch req.Strategy {
		case "all_pass":
			res.AllPass = true
		case "any_pass":
			res.AnyPass = false
		}

		return res, nil
	}

	for i := range req.Items {
		if req.Items[i].OK {
			res.Succeeded++
		} else {
			res.Failed++
		}
	}

	switch req.Strategy {
	case "collect":
		res.Results = make([]any, 0, res.Succeeded)

		for i := range req.Items {
			if req.Items[i].OK {
				res.Results = append(res.Results, req.Items[i].Result)
			}
		}
	case "all_pass":
		res.AllPass = res.Failed == 0
	case "any_pass":
		res.AnyPass = res.Succeeded > 0
	case "first_success":
		for i := range req.Items {
			if req.Items[i].OK {
				res.Result = req.Items[i].Result

				return res, nil
			}
		}

		return res, xerr.NotFound("distribute.reduce: no successful item")
	case "count":
		// Succeeded / Failed already populated above.
	default:
		return res, xerr.BadRequest("distribute.reduce: unknown strategy: " + req.Strategy)
	}

	return res, nil
}

func DistributeMaxItemsForTest() int { return distributeMaxItems }
