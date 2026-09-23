package dslparse

import (
	"strconv"
	"strings"
)

// Security, HITL, budget, and effect modifiers.
//
//	:auth, :role=, :perm=, :permission=, :feature=
//	:budget_micros=, :budget=, :audit
//	:hitl=, :hitl_options=, :hitl_trigger=, :hitl_triggers=
//	:gate=, :effect=, :read_only
func init() {
	registerMod("auth", func(p *Modifiers, v string, has bool) error {
		if !has {
			p.RequiresAuth = true
			return nil
		}
		switch toLower(trimValue(v)) {
		case "required", "true", "yes", "1":
			p.RequiresAuth = true
		}
		return nil
	})

	registerMod("role", func(p *Modifiers, v string, _ bool) error {
		p.Roles = append(p.Roles, splitCSV(v)...)
		return nil
	})

	perm := func(p *Modifiers, v string, _ bool) error {
		p.Permissions = append(p.Permissions, splitCSV(v)...)
		return nil
	}
	registerMod("perm", perm)
	registerMod("permission", perm)

	registerMod("feature", func(p *Modifiers, v string, _ bool) error {
		p.Features = append(p.Features, splitCSV(v)...)
		return nil
	})

	registerMod("budget_micros", func(p *Modifiers, v string, _ bool) error {
		n, err := strconv.ParseInt(trimValue(v), 10, 64)
		if err != nil {
			return err
		}
		p.BudgetMicros = n
		return nil
	})

	registerMod("budget", func(p *Modifiers, v string, _ bool) error {
		if micros, ok := parseBudget(v); ok {
			p.BudgetMicros = micros
		}
		return nil
	})

	registerMod("audit", func(p *Modifiers, v string, has bool) error {
		if !has {
			p.Audit = true
			return nil
		}
		switch toLower(trimValue(v)) {
		case "true", "yes", "1":
			p.Audit = true
		}
		return nil
	})

	registerMod("hitl", func(p *Modifiers, v string, _ bool) error {
		p.HITLPrompt = trimValue(v)
		return nil
	})

	registerMod("hitl_options", func(p *Modifiers, v string, _ bool) error {
		p.HITLOptions = append(p.HITLOptions, splitCSV(v)...)
		return nil
	})

	hitlTrigger := func(p *Modifiers, v string, _ bool) error {
		p.HITLTriggers = append(p.HITLTriggers, splitCSV(v)...)
		return nil
	}
	registerMod("hitl_trigger", hitlTrigger)
	registerMod("hitl_triggers", hitlTrigger)

	registerMod("gate", func(p *Modifiers, v string, _ bool) error {
		p.Gate = trimValue(v)
		return nil
	})

	registerMod("effect", func(p *Modifiers, v string, _ bool) error {
		e := toLower(trimValue(v))
		switch e {
		case "read_only", "side_effect", "high_risk":
			p.Effect = e
		}
		return nil
	})

	registerMod("read_only", func(p *Modifiers, _ string, _ bool) error {
		p.ReadOnly = true
		p.Effect = "read_only"
		return nil
	})
}

func parseBudget(raw string) (int64, bool) {
	raw = trimValue(raw)
	if raw == "" {
		return 0, false
	}
	if strings.HasPrefix(raw, "$") || strings.Contains(raw, ".") {
		f, err := strconv.ParseFloat(strings.TrimPrefix(raw, "$"), 64)
		if err != nil {
			return 0, false
		}
		return int64(f * 1_000_000), true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
