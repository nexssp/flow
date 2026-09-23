package dslparse

import (
	"strconv"
	"strings"
)

// Identity, lifecycle, and metadata modifiers.
//
//	:name=, :desc=, :description=, :type=, :scope=, :status=, :tag=
//	:deprecated, :since=, :use=, :debug, :validate
func init() {
	registerMod("name", func(p *Modifiers, v string, has bool) error {
		p.CustomName = trimValue(v)
		return nil
	})

	desc := func(p *Modifiers, v string, has bool) error {
		p.Description = trimValue(v)
		return nil
	}
	registerMod("desc", desc)
	registerMod("description", desc)

	registerMod("type", func(p *Modifiers, v string, has bool) error {
		val := trimValue(v)
		if arrow := strings.Index(val, "->"); arrow >= 0 {
			p.ReqType = strings.TrimSpace(val[:arrow])
			p.ResType = strings.TrimSpace(val[arrow+2:])
		} else if val != "" {
			p.ReqType = val
		}
		return nil
	})

	registerMod("scope", func(p *Modifiers, v string, has bool) error {
		p.Scope = toLower(trimValue(v))
		return nil
	})

	registerMod("status", func(p *Modifiers, v string, has bool) error {
		n, err := strconv.Atoi(trimValue(v))
		if err != nil {
			return err
		}
		p.SuccessStatus = n
		return nil
	})

	registerMod("tag", func(p *Modifiers, v string, has bool) error {
		p.Tags = append(p.Tags, splitCSV(v)...)
		return nil
	})

	registerMod("deprecated", func(p *Modifiers, _ string, _ bool) error {
		p.Deprecated = true
		return nil
	})

	registerMod("since", func(p *Modifiers, v string, _ bool) error {
		p.DeprecatedSince = trimValue(v)
		return nil
	})

	registerMod("use", func(p *Modifiers, v string, _ bool) error {
		p.DeprecatedUse = trimValue(v)
		return nil
	})

	registerMod("debug", func(p *Modifiers, _ string, _ bool) error {
		p.Debug = true
		return nil
	})

	registerMod("validate", func(p *Modifiers, _ string, _ bool) error {
		p.Validate = true
		return nil
	})
}
