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
