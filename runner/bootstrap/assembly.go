package bootstrap

import "github.com/nexssp/kernel/action"

type loader func(*Assembly) error

type Assembly struct {
	Actions []action.AnyAction
}

func newAssembly() *Assembly {
	return &Assembly{}
}

func (a *Assembly) finalize() []action.AnyAction {
	lastIdx := make(map[string]int, len(a.Actions))
	for i, act := range a.Actions {
		if act == nil || act.Describe() == nil {
			continue
		}
		lastIdx[act.Describe().Name] = i
	}

	out := make([]action.AnyAction, 0, len(lastIdx))
	for i, act := range a.Actions {
		if act == nil || act.Describe() == nil {
			continue
		}
		if lastIdx[act.Describe().Name] != i {
			continue
		}
		out = append(out, act)
	}
	return out
}
