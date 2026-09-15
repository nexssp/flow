package flow

import (
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// RegisterPipelines installs named pipelines into an existing registry.
//
// Each pipeline becomes a Proxy action under its own name, so pipelines
// can reference each other and any other action regardless of
// declaration order. Cycles between pipelines are detected before any
// compilation runs. The registry is mutated in place.
func RegisterPipelines(reg *MapRegistry, pipelines []Pipeline) error {
	if len(pipelines) == 0 {
		return nil
	}

	if err := detectPipelineCycles(pipelines); err != nil {
		return err
	}

	proxies := make(map[string]*action.Proxy, len(pipelines))
	for _, p := range pipelines {
		if _, exists := reg.Get(p.Name); exists {
			return xerr.Conflict("flow: pipeline " + p.Name + " collides with an existing action")
		}

		proxy := action.NewProxy(nil)
		proxies[p.Name] = proxy
		reg.Register(p.Name, proxy)
	}

	for _, p := range pipelines {
		bld, err := CompilePipeline(p.Body, reg)
		if err != nil {
			return fmt.Errorf("flow: compile pipeline %q: %w", p.Name, err)
		}

		proxies[p.Name].Swap(bld.Build())
	}

	return nil
}

func detectPipelineCycles(pipelines []Pipeline) error {
	byName := make(map[string]Pipeline, len(pipelines))
	for _, p := range pipelines {
		byName[p.Name] = p
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)

	color := make(map[string]int, len(pipelines))

	var visit func(name string, stack []string) error

	visit = func(name string, stack []string) error {
		switch color[name] {
		case gray:
			return xerr.Conflict("flow: pipeline cycle: " +
				strings.Join(append(stack, name), " → "))
		case black:
			return nil
		}

		color[name] = gray

		p, ok := byName[name]
		if !ok {
			color[name] = black

			return nil
		}

		for ref := range referencedAtoms(p.Body) {
			if _, isPipeline := byName[ref]; !isPipeline {
				continue
			}

			if err := visit(ref, append(stack, name)); err != nil {
				return err
			}
		}

		color[name] = black

		return nil
	}

	for _, p := range pipelines {
		if err := visit(p.Name, nil); err != nil {
			return err
		}
	}

	return nil
}

// referencedAtoms conservatively extracts identifiers that could be
// action names inside a DSL body. This is a set, not a data model.
func referencedAtoms(body string) map[string]struct{} {
	out := make(map[string]struct{}, 8)

	splitter := func(r rune) bool {
		switch r {
		case '>', '|', '&', '\n', '(', ')', '{', '}':
			return true
		}

		return false
	}
	for _, raw := range strings.FieldsFunc(body, splitter) {
		atom := strings.TrimSpace(raw)
		atom = strings.TrimPrefix(atom, "-")

		atom = strings.TrimSpace(atom)
		if atom == "" {
			continue
		}

		if idx := strings.IndexByte(atom, ':'); idx > 0 {
			atom = atom[:idx]
		}

		if atom == "" || strings.ContainsAny(atom, " \"'") {
			continue
		}

		out[atom] = struct{}{}
	}

	return out
}
