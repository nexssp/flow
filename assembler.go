package flow

import (
	"fmt"
	"os"
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

type SystemAssembler struct {
	actions  []action.AnyAction
	aliases  []action.Alias
	compiled []action.AnyAction
}

func NewAssembler(capabilities ...action.AnyAction) *SystemAssembler {
	return &SystemAssembler{
		actions:  append([]action.AnyAction(nil), capabilities...),
		compiled: make([]action.AnyAction, 0, 16),
	}
}

func (s *SystemAssembler) Register(name string, act action.AnyAction) *SystemAssembler {
	if act == nil {
		return s
	}
	s.actions = append(s.actions, act)
	if name != "" && name != act.Describe().Name {
		s.aliases = append(s.aliases, action.Alias{
			Canonical: act.Describe().Name,
			Short:     []string{name},
		})
	}
	return s
}

func (s *SystemAssembler) AssembleFile(path string) ([]action.AnyAction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("flow: read file %q: %w", path, err)
	}
	return s.AssembleManifest(string(data))
}

func (s *SystemAssembler) AssembleManifest(manifestDSL string) ([]action.AnyAction, error) {
	reg, err := action.NewRegistry(action.Library{
		Name:    "assembler",
		Actions: s.actions,
		Aliases: s.aliases,
	})
	if err != nil {
		return nil, fmt.Errorf("flow: assemble registry: %w", err)
	}

	for _, rawLine := range strings.Split(manifestDSL, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		parser := compiler.NewParser(line)
		ast, err := parser.ParseExpression()
		if err != nil {
			return nil, fmt.Errorf("flow: assemble %q failed: %w", line, err)
		}
		builder, err := compileAST(ast, reg)
		if err != nil {
			return nil, fmt.Errorf("flow: assemble %q failed: %w", line, err)
		}
		s.compiled = append(s.compiled, builder.Build())
	}
	return s.compiled, nil
}

func (s *SystemAssembler) Actions() []action.AnyAction {
	return s.compiled
}
