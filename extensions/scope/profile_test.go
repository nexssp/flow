package scope_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestProfile_DeclaredAndUsed(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@profile reliable :tag="prof-tag"

@pipeline fetch :profile=reliable
  const @{ value: "ok" }
@end

{} -> pipeline.fetch
`
	ex := mustRun(t, cfg, src)
	ktest.RequireEqual(t, ex.Output, "ok")

	act, found := ex.Resolver.Action("pipeline.fetch")
	ktest.RequireCondition(t, found, "pipeline.fetch not mounted")

	hasTag := false
	for _, tag := range act.Describe().Tags {
		if tag == "prof-tag" {
			hasTag = true
		}
	}
	ktest.RequireCondition(t, hasTag,
		"expected prof-tag on pipeline.fetch, got %v", act.Describe().Tags)
}

func TestProfile_LocalOverridesProfile(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@profile reliable :tag="from-profile"

@pipeline fetch :profile=reliable :tag="local"
  const @{ value: "ok" }
@end

{} -> pipeline.fetch
`
	ex := mustRun(t, cfg, src)

	act, found := ex.Resolver.Action("pipeline.fetch")
	ktest.RequireCondition(t, found, "pipeline.fetch not mounted")

	hasLocal, hasProfile := false, false
	for _, tag := range act.Describe().Tags {
		switch tag {
		case "local":
			hasLocal = true
		case "from-profile":
			hasProfile = true
		}
	}
	ktest.RequireCondition(t, hasLocal,
		"expected :tag=local to win, got %v", act.Describe().Tags)
	ktest.RequireCondition(t, !hasProfile,
		"profile-supplied tag should be overridden, got %v", act.Describe().Tags)
}

func TestProfile_EmptyProfile(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@profile empty

@pipeline work :profile=empty
  const @{ value: "ok" }
@end

{} -> pipeline.work
`
	ex := mustRun(t, cfg, src)
	ktest.RequireEqual(t, ex.Output, "ok")
}

func TestProfile_UnknownRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@pipeline fetch :profile=missing
  const @{ value: "ok" }
@end
`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected unknown profile error")
	ktest.RequireStringContains(t, err.Error(), "unknown profile")
}

func TestProfile_DuplicateRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `@profile dup :tag="a"
@profile dup :tag="b"`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected duplicate error")
	ktest.RequireStringContains(t, err.Error(), "duplicate")
}

func TestProfile_InvalidNameRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `@profile 1bad :tag="a"`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected invalid name error")
	ktest.RequireStringContains(t, err.Error(), "invalid name")
}

func TestProfile_ParentInherits(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@profile base :tag="from-base"
@profile child :parent=base

@pipeline work :profile=child
  const @{ value: "ok" }
@end

{} -> pipeline.work
`
	ex := mustRun(t, cfg, src)

	act, found := ex.Resolver.Action("pipeline.work")
	ktest.RequireCondition(t, found, "pipeline.work not mounted")

	hasTag := false
	for _, tag := range act.Describe().Tags {
		if tag == "from-base" {
			hasTag = true
		}
	}
	ktest.RequireCondition(t, hasTag,
		"parent :tag must reach pipeline.work, got %v", act.Describe().Tags)
}

func TestProfile_ChildOverridesParent(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@profile base :tag="base-tag"
@profile child :parent=base :tag="child-tag"

@pipeline work :profile=child
  const @{ value: "ok" }
@end

{} -> pipeline.work
`
	ex := mustRun(t, cfg, src)

	act, found := ex.Resolver.Action("pipeline.work")
	ktest.RequireCondition(t, found, "pipeline.work not mounted")

	hasChild, hasBase := false, false
	for _, tag := range act.Describe().Tags {
		switch tag {
		case "child-tag":
			hasChild = true
		case "base-tag":
			hasBase = true
		}
	}
	ktest.RequireCondition(t, hasChild, "child tag must win, got %v", act.Describe().Tags)
	ktest.RequireCondition(t, !hasBase,
		"parent tag must be overridden by child, got %v", act.Describe().Tags)
}

func TestProfile_CycleRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@profile a :parent=b
@profile b :parent=a

@pipeline work :profile=a
  const @{ value: "ok" }
@end

{} -> pipeline.work
`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected cycle error")
	ktest.RequireStringContains(t, err.Error(), "cycle")
}

func TestProfile_SelfParentRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@profile self :parent=self

@pipeline work :profile=self
  const @{ value: "ok" }
@end

{} -> pipeline.work
`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected cycle error")
	ktest.RequireStringContains(t, err.Error(), "cycle")
}

func TestProfile_UnknownParentRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@profile child :parent=missing

@pipeline work :profile=child
  const @{ value: "ok" }
@end

{} -> pipeline.work
`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected unknown parent error")
	ktest.RequireStringContains(t, err.Error(), "unknown parent")
}

func TestProfile_DuplicateParentRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `@profile bad :parent=a :parent=b`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected duplicate parent error")
	ktest.RequireStringContains(t, err.Error(), "at most once")
}
