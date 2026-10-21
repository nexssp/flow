package compiler_test

import (
	"strconv"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestValidateBundleMaterializePhase(t *testing.T) {
	t.Parallel()
	for _, phase := range []compiler.MaterializePhase{compiler.MaterializeDeclarations, compiler.MaterializeCompositions} {
		t.Run(strconv.Itoa(int(phase)), func(t *testing.T) {
			t.Parallel()
			ktest.RequireNoError(t, compiler.ValidateBundle(compiler.Bundle{ID: "extension", MaterializePhase: phase}))
		})
	}
	for _, phase := range []compiler.MaterializePhase{-1, 2, 99} {
		err := compiler.ValidateBundle(compiler.Bundle{ID: "extension", MaterializePhase: phase})
		ktest.RequireErrorContains(t, err, "invalid materialize phase")
		ktest.RequireErrorKind(t, err, xerr.KindValidation)
	}
	ktest.RequireEqual(t, compiler.Bundle{}.MaterializePhase, compiler.MaterializeDeclarations)
}
