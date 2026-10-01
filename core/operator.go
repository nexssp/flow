package core

import (
	"context"

	"github.com/nexssp/kernel/action"
)

type Associativity uint8

const (
	Left Associativity = iota
	Right
)

type OperatorMeta struct {
	Name          string
	Token         TokenType
	Precedence    int
	Associativity Associativity
	Description   string
	Example       string
}

type OperatorReq struct {
	Left  Expr
	Right Expr
	Meta  *OperatorMeta
}

type OperatorRes struct {
	Node Expr
}

type Operator struct {
	Meta    OperatorMeta
	Handler func(context.Context, OperatorReq) (OperatorRes, error)
}

func (o Operator) Action() *action.BuiltAction[OperatorReq, OperatorRes] {
	return action.New("operator."+o.Meta.Name, o.Handler).
		Description(o.Meta.Description).
		Tag("operator", o.Meta.Name).
		Build()
}

type OperatorTable struct {
	byToken map[TokenType]operatorEntry
	byName  map[string]operatorEntry
	ordered []operatorEntry
}

type operatorEntry struct {
	meta   OperatorMeta
	action *action.BuiltAction[OperatorReq, OperatorRes]
}

func NewOperatorTable(ops ...Operator) *OperatorTable {
	t := &OperatorTable{
		byToken: make(map[TokenType]operatorEntry, len(ops)),
		byName:  make(map[string]operatorEntry, len(ops)),
		ordered: make([]operatorEntry, 0, len(ops)),
	}
	for _, op := range ops {
		if op.Meta.Name == "" {
			panic("core: operator with empty Name")
		}
		if _, dup := t.byName[op.Meta.Name]; dup {
			panic("core: duplicate operator " + op.Meta.Name)
		}
		if _, dup := t.byToken[op.Meta.Token]; dup {
			panic("core: duplicate token " + op.Meta.Token.String())
		}
		e := operatorEntry{meta: op.Meta, action: op.Action()}
		t.byToken[op.Meta.Token] = e
		t.byName[op.Meta.Name] = e
		t.ordered = append(t.ordered, e)
	}
	return t
}

func (t *OperatorTable) ByToken(tok TokenType) (OperatorMeta, *action.BuiltAction[OperatorReq, OperatorRes], bool) {
	e, ok := t.byToken[tok]
	if !ok {
		return OperatorMeta{}, nil, false
	}
	return e.meta, e.action, true
}

func (t *OperatorTable) ByName(name string) (OperatorMeta, *action.BuiltAction[OperatorReq, OperatorRes], bool) {
	e, ok := t.byName[name]
	if !ok {
		return OperatorMeta{}, nil, false
	}
	return e.meta, e.action, true
}

func (t *OperatorTable) All() []OperatorMeta {
	out := make([]OperatorMeta, len(t.ordered))
	for i, e := range t.ordered {
		out[i] = e.meta
	}
	return out
}

func (t *OperatorTable) Actions() []action.AnyAction {
	out := make([]action.AnyAction, len(t.ordered))
	for i, e := range t.ordered {
		out[i] = e.action
	}
	return out
}
