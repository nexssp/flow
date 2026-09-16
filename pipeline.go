package flow

import (
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

func RegisterPipelines(reg *action.Registry, pipelines []Pipeline) (*action.Registry, error) {
	if len(pipelines) == 0 {
		return reg, nil
	}
	if err := detectPipelineCycles(pipelines); err != nil {
		return nil, err
	}

	proxies := make([]*action.Proxy, len(pipelines))
	proxyActions := make([]action.AnyAction, len(pipelines))

	for i := range pipelines {
		if reg != nil {
			if _, exists := reg.Get(pipelines[i].Name); exists {
				return nil, xerr.Conflict("flow: pipeline " + pipelines[i].Name + " collides with an existing action")
			}
		}
		placeholder := action.New[any, any](pipelines[i].Name, nil).Build()
		p := action.NewProxy(placeholder)
		proxies[i] = p
		proxyActions[i] = p
	}

	libs := make([]action.Library, 0, 2)
	if reg != nil && len(reg.Actions()) > 0 {
		libs = append(libs, action.Library{Name: "base", Actions: reg.Actions()})
	}
	libs = append(libs, action.Library{Name: "pipeline_proxies", Actions: proxyActions})

	intermediate, err := action.NewRegistry(libs...)
	if err != nil {
		return nil, fmt.Errorf("flow: build intermediate registry: %w", err)
	}

	for i := range pipelines {
		builder, err := CompilePipeline(pipelines[i].Body, intermediate)
		if err != nil {
			return nil, fmt.Errorf("flow: compile pipeline %q: %w", pipelines[i].Name, err)
		}
		builder.Name(pipelines[i].Name)
		proxies[i].Swap(builder.Build())
	}

	finalLibs := make([]action.Library, 0, 2)
	if reg != nil && len(reg.Actions()) > 0 {
		finalLibs = append(finalLibs, action.Library{Name: "base", Actions: reg.Actions()})
	}
	finalLibs = append(finalLibs, action.Library{Name: "pipelines", Actions: proxyActions})

	return action.NewRegistry(finalLibs...)
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
			return xerr.Conflict("flow: pipeline cycle: " + strings.Join(append(stack, name), " → "))
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
