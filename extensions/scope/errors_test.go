package scope_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestError_UnknownProfileMentionsName(t *testing.T) {
	cfg := buildConfig(t)
	src := `
@pipeline work :profile=missing
  const @{ value: 1 }
@end
`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected unknown profile error")
	ktest.RequireStringContains(t, err.Error(), "missing")
}

func TestError_DuplicateProfileMentionsName(t *testing.T) {
	cfg := buildConfig(t)
	src := `@profile dup :tag="a"
@profile dup :tag="b"`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected duplicate error")
	ktest.RequireStringContains(t, err.Error(), "dup")
}
