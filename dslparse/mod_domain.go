package dslparse

import "strconv"

// Domain-specific modifiers. These are parsed here so the compiler can
// route on them, but their meaning is defined by the domain package
// that consumes them:
//
//	LLM (ai/llm):
//	  :provider=, :model=, :typed=, :system=, :tools=, :max_turns=
//
//	Sandbox (ai/sandbox):
//	  :image=, :skills=
func init() {
	registerMod("provider", func(p *Modifiers, v string, _ bool) error {
		p.Provider = trimValue(v)
		return nil
	})

	registerMod("model", func(p *Modifiers, v string, _ bool) error {
		p.Model = trimValue(v)
		return nil
	})

	registerMod("typed", func(p *Modifiers, v string, _ bool) error {
		p.Typed = trimValue(v)
		return nil
	})

	registerMod("schema", func(p *Modifiers, v string, _ bool) error {
		p.Schema = trimValue(v)
		return nil
	})

	registerMod("system", func(p *Modifiers, v string, _ bool) error {
		p.System = trimValue(v)
		return nil
	})

	registerMod("tools", func(p *Modifiers, v string, _ bool) error {
		p.Tools = append(p.Tools, splitCSV(v)...)
		return nil
	})

	registerMod("max_turns", func(p *Modifiers, v string, _ bool) error {
		n, err := strconv.Atoi(trimValue(v))
		if err != nil {
			return err
		}
		p.MaxTurns = n
		return nil
	})

	registerMod("image", func(p *Modifiers, v string, _ bool) error {
		p.Image = trimValue(v)
		return nil
	})

	registerMod("skills", func(p *Modifiers, v string, _ bool) error {
		p.Skills = append(p.Skills, splitCSV(v)...)
		return nil
	})
}
