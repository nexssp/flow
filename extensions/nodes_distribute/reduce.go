package nodes_distribute

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// DistributeReduceReq is the reduce configuration. Strategy selects
// the fold: collect, all_pass, any_pass, first_success, count.
type DistributeReduceReq struct {
	Strategy string `json:"strategy" validate:"required,oneof=collect all_pass any_pass first_success count"`
	Items    []Item `json:"items"    validate:"required"`
}

// DistributeReduceRes carries only the field selected by the strategy.
type DistributeReduceRes struct {
	Strategy  string `json:"strategy"`
	AllPass   bool   `json:"all_pass,omitempty"`
	AnyPass   bool   `json:"any_pass,omitempty"`
	Succeeded int    `json:"succeeded,omitempty"`
	Failed    int    `json:"failed,omitempty"`
	Result    any    `json:"result,omitempty"`
	Results   []any  `json:"results,omitempty"`
}

// DistributeReduce folds a distribute.map result into a single value.
var DistributeReduce = action.New("distribute.reduce", func(_ context.Context, req DistributeReduceReq) (DistributeReduceRes, error) {
	return runReduce(req)
}).Description("Fold distribute.map output into a single value").
	Tag("distribute", "fold").
	Build()

func runReduce(req DistributeReduceReq) (DistributeReduceRes, error) {
	result := DistributeReduceRes{Strategy: req.Strategy}

	for i := range req.Items {
		if req.Items[i].OK {
			result.Succeeded++
		} else {
			result.Failed++
		}
	}

	switch req.Strategy {
	case "collect":
		result.Results = make([]any, 0, result.Succeeded)
		for i := range req.Items {
			if req.Items[i].OK {
				result.Results = append(result.Results, req.Items[i].Result)
			}
		}
	case "all_pass":
		result.AllPass = result.Failed == 0
	case "any_pass":
		result.AnyPass = result.Succeeded > 0
	case "first_success":
		for i := range req.Items {
			if req.Items[i].OK {
				result.Result = req.Items[i].Result
				return result, nil
			}
		}
		return result, xerr.NotFound("distribute.reduce: no successful item")
	case "count":
		// Succeeded and Failed already populated.
	default:
		return result, xerr.BadRequest("distribute.reduce: unknown strategy: " + req.Strategy)
	}
	return result, nil
}
