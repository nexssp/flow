package nodes_distribute

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestReduce_Collect(t *testing.T) {
	t.Parallel()
	got, err := runReduce(DistributeReduceReq{
		Strategy: "collect",
		Items: []Item{
			{OK: true, Result: 1},
			{OK: true, Result: 2},
			{OK: false, Error: "boom"},
		},
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got.Succeeded, 2)
	ktest.RequireEqual(t, got.Failed, 1)
	ktest.RequireEqual(t, got.Results, []any{1, 2})
}

func TestReduce_AllPass(t *testing.T) {
	t.Parallel()
	got, _ := runReduce(DistributeReduceReq{
		Strategy: "all_pass",
		Items:    []Item{{OK: true}, {OK: true}},
	})
	ktest.RequireCondition(t, got.AllPass, "expected all_pass=true")

	got, _ = runReduce(DistributeReduceReq{
		Strategy: "all_pass",
		Items:    []Item{{OK: true}, {OK: false}},
	})
	ktest.RequireCondition(t, !got.AllPass, "expected all_pass=false")
}

func TestReduce_AnyPass(t *testing.T) {
	t.Parallel()
	got, _ := runReduce(DistributeReduceReq{
		Strategy: "any_pass",
		Items:    []Item{{OK: false}, {OK: true}},
	})
	ktest.RequireCondition(t, got.AnyPass, "expected any_pass=true")
}

func TestReduce_FirstSuccess(t *testing.T) {
	t.Parallel()
	got, err := runReduce(DistributeReduceReq{
		Strategy: "first_success",
		Items: []Item{
			{OK: false, Error: "x"},
			{OK: true, Result: "winner"},
			{OK: true, Result: "ignored"},
		},
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got.Result, "winner")
}

func TestReduce_FirstSuccess_AllFailed(t *testing.T) {
	t.Parallel()
	_, err := runReduce(DistributeReduceReq{
		Strategy: "first_success",
		Items:    []Item{{OK: false}, {OK: false}},
	})
	ktest.RequireErrorKind(t, err, xerr.KindNotFound)
}

func TestReduce_Count(t *testing.T) {
	t.Parallel()
	got, _ := runReduce(DistributeReduceReq{
		Strategy: "count",
		Items:    []Item{{OK: true}, {OK: false}, {OK: true}},
	})
	ktest.RequireEqual(t, got.Succeeded, 2)
	ktest.RequireEqual(t, got.Failed, 1)
}

func TestReduce_UnknownStrategy(t *testing.T) {
	t.Parallel()
	_, err := runReduce(DistributeReduceReq{Strategy: "nope"})
	ktest.RequireErrorKind(t, err, xerr.KindBadRequest)
}

func TestBundle_WiresActionsAndFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")

	names := map[string]bool{"distribute.map": false, "distribute.reduce": false}
	for _, a := range Library().Actions {
		if _, ok := names[a.Describe().Name]; ok {
			names[a.Describe().Name] = true
		}
	}
	for name, seen := range names {
		ktest.RequireCondition(t, seen, "action %q missing", name)
	}
}

var _ = context.Background
