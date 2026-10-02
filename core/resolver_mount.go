package core

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"strings"
	"unicode"

	"github.com/nexssp/kernel/action"
)

type resolverOwner struct {
	kind    string
	library string
}

type stagedLibraryMount struct {
	actions   []action.AnyAction
	sources   []action.AnyStreamAction
	operators []action.NamedOperator
	names     []string
	owners    map[string]resolverOwner
}

func (r *DynamicResolver) Mount(lib action.Library) error {
	return r.mount(lib, "")
}

func (r *DynamicResolver) MountWithAlias(lib action.Library, alias string) error {
	if alias == "" {
		return r.Mount(lib)
	}
	if !validNamespaceQualifier(alias) {
		return fmt.Errorf("library %q: invalid @require namespace qualifier %q", lib.Name, alias)
	}
	return r.mount(lib, alias)
}

func (r *DynamicResolver) mount(lib action.Library, alias string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mountLocked(lib, alias)
}

func (r *DynamicResolver) mountLocked(lib action.Library, alias string) error {
	mountKey := lib.Name
	owner := lib.Name
	if alias != "" {
		mountKey += "\x00" + alias
		owner = fmt.Sprintf("%s as %s", lib.Name, alias)
	}
	if previous, exists := r.mounts[mountKey]; exists {
		return fmt.Errorf("duplicate mount of library %q with qualifier %q (previous owner %q)", lib.Name, alias, previous)
	}

	staged, err := stageLibraryMount(lib, alias, owner)
	if err != nil {
		return err
	}
	for _, name := range staged.names {
		incoming := staged.owners[name]
		if previous, exists := r.owners[name]; exists {
			return fmt.Errorf("canonical collision: name %q kind %s owner %q conflicts with kind %s owner %q",
				name, incoming.kind, incoming.library, previous.kind, previous.library)
		}
	}

	for _, act := range staged.actions {
		name := act.Describe().Name
		r.actions[name] = act
		r.owners[name] = staged.owners[name]
	}
	for _, source := range staged.sources {
		name := source.Describe().Name
		r.streams[name] = source
		r.owners[name] = staged.owners[name]
	}
	for _, operator := range staged.operators {
		r.operators[operator.Name] = operator
		r.owners[operator.Name] = staged.owners[operator.Name]
	}
	r.mounts[mountKey] = owner
	return nil
}

func stageLibraryMount(lib action.Library, alias, owner string) (*stagedLibraryMount, error) {
	if strings.TrimSpace(lib.Name) == "" {
		return nil, errors.New("library: name is required")
	}
	if len(lib.Aliases) > 0 {
		return nil, fmt.Errorf("library %q declares Kernel action aliases; Flow only permits local @require ... as namespace qualifiers", lib.Name)
	}
	if len(lib.Overrides) > 0 {
		return nil, fmt.Errorf("library %q declares Kernel Overrides, which are not supported by Flow mounting", lib.Name)
	}

	staged := &stagedLibraryMount{
		actions:   make([]action.AnyAction, 0, len(lib.Actions)),
		sources:   make([]action.AnyStreamAction, 0, len(lib.Sources)),
		operators: make([]action.NamedOperator, 0, len(lib.Operators)),
		owners:    make(map[string]resolverOwner, len(lib.Actions)+len(lib.Sources)+len(lib.Operators)),
	}
	if err := stageActions(lib, alias, owner, staged); err != nil {
		return nil, err
	}
	if err := stageSources(lib, alias, owner, staged); err != nil {
		return nil, err
	}
	if err := stageOperators(lib, alias, owner, staged); err != nil {
		return nil, err
	}
	return staged, nil
}

func stageActions(lib action.Library, alias, owner string, staged *stagedLibraryMount) error {
	for i, raw := range lib.Actions {
		if raw == nil {
			return fmt.Errorf("library %q: action %d is nil", lib.Name, i)
		}
		meta := raw.Describe()
		if meta == nil {
			return fmt.Errorf("library %q: action %d has no metadata", lib.Name, i)
		}
		name := mountedCapabilityName(meta.Name, alias)
		if err := addStagedName(staged, lib.Name, owner, "action", meta.Name, name); err != nil {
			return err
		}
		act := raw
		if name != meta.Name {
			act = action.Dynamic(raw).Name(name).Build()
		}
		staged.actions = append(staged.actions, act.CloneWithHooks(lib.Hooks...))
	}
	return nil
}

func stageSources(lib action.Library, alias, owner string, staged *stagedLibraryMount) error {
	for i, raw := range lib.Sources {
		if raw == nil {
			return fmt.Errorf("library %q: source %d is nil", lib.Name, i)
		}
		meta := raw.Describe()
		if meta == nil {
			return fmt.Errorf("library %q: source %d has no metadata", lib.Name, i)
		}
		name := mountedCapabilityName(meta.Name, alias)
		if err := addStagedName(staged, lib.Name, owner, "source", meta.Name, name); err != nil {
			return err
		}
		source := raw.CloneWithHooks(lib.Hooks...)
		if name != meta.Name {
			source = &qualifiedStream{inner: source, name: name}
		}
		staged.sources = append(staged.sources, source)
	}
	return nil
}

func stageOperators(lib action.Library, alias, owner string, staged *stagedLibraryMount) error {
	for i, raw := range lib.Operators {
		if err := action.ValidateOperatorDeclaration(raw); err != nil {
			return fmt.Errorf("library %q operator %d: %w", lib.Name, i, err)
		}
		name := mountedCapabilityName(raw.Name, alias)
		if err := addStagedName(staged, lib.Name, owner, "operator", raw.Name, name); err != nil {
			return err
		}
		op := raw.Clone()
		if name != op.Name {
			build := op.Build
			op.Name = name
			op.Build = func(params any) (action.StreamOperator, error) {
				inner, err := build(params)
				if err != nil {
					return nil, err
				}
				return qualifiedOperator{inner: inner, name: name}, nil
			}
		}
		staged.operators = append(staged.operators, op)
	}
	return nil
}

func addStagedName(staged *stagedLibraryMount, library, owner, kind, declared, mounted string) error {
	if !validQualifiedCapabilityName(declared) {
		return fmt.Errorf("library %q: %s %q must declare one canonical fully-qualified name", library, kind, declared)
	}
	if previous, exists := staged.owners[mounted]; exists {
		return fmt.Errorf("canonical collision: name %q kind %s owner %q conflicts with kind %s owner %q in the same library mount",
			mounted, kind, owner, previous.kind, previous.library)
	}
	staged.names = append(staged.names, mounted)
	staged.owners[mounted] = resolverOwner{kind: kind, library: owner}
	return nil
}

func mountedCapabilityName(name, alias string) string {
	if alias == "" {
		return name
	}
	return replaceCapabilityNamespace(name, alias)
}

func validQualifiedCapabilityName(name string) bool {
	return strings.TrimSpace(name) != "" && strings.TrimSpace(name) == name &&
		strings.IndexFunc(name, unicode.IsSpace) < 0 &&
		!strings.HasPrefix(name, ".") && !strings.HasSuffix(name, ".") && !strings.Contains(name, "..") &&
		strings.Contains(name, ".")
}

func replaceCapabilityNamespace(name, qualifier string) string {
	_, suffix, hasNamespace := strings.Cut(name, ".")
	if !hasNamespace {
		return qualifier + "." + name
	}
	return qualifier + "." + suffix
}

func validNamespaceQualifier(alias string) bool {
	if alias == "" || alias == "_" || token.Lookup(alias).IsKeyword() {
		return false
	}
	for i, r := range alias {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

type qualifiedOperator struct {
	inner action.StreamOperator
	name  string
}

func (o qualifiedOperator) Name() string { return o.name }
func (o qualifiedOperator) Apply(up action.AnyStream) (action.AnyStream, error) {
	return o.inner.Apply(up)
}

type qualifiedStream struct {
	inner action.AnyStreamAction
	name  string
}

func (s *qualifiedStream) Describe() *action.Meta {
	meta := *s.inner.Describe()
	meta.Name = s.name
	return &meta
}
func (s *qualifiedStream) ReqPayload() any               { return s.inner.ReqPayload() }
func (s *qualifiedStream) ResPayload() any               { return s.inner.ResPayload() }
func (s *qualifiedStream) GetBindings() []action.Binding { return s.inner.GetBindings() }
func (s *qualifiedStream) GetAnyHooks() []action.AnyHook { return s.inner.GetAnyHooks() }
func (s *qualifiedStream) AddAnyHook(hooks ...action.AnyHook) {
	s.inner.AddAnyHook(hooks...)
}

func (s *qualifiedStream) CloneWithHooks(hooks ...action.AnyHook) action.AnyStreamAction {
	return &qualifiedStream{inner: s.inner.CloneWithHooks(hooks...), name: s.name}
}

func (s *qualifiedStream) DoStreamAny(ctx context.Context, req any) (action.AnyStream, error) {
	return s.inner.DoStreamAny(ctx, req)
}
