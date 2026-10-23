package require

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	includeext "github.com/nexssp/flow/extensions/include"
)

func TestDirectDeclarationsCaptureOriginalSourceAndOnlyDirectHandlers(t *testing.T) {
	t.Parallel()

	root := "@require\tgithub.com/nexssp/aliased as ali v1.2.3 { id: \"root\" } # trailing // comment\r\n" +
		"@require github.com/nexssp/aliased as ali v1.2.3 { id: \"root\" }\r\n" +
		"@require\thttps://example.com/foo 'feature/branch' // trailing comment\r\n" +
		"@require\tgithub.com/nexssp/bare as bare # comment\r\n" +
		"@include child.nflow\r\n"
	child := "@require github.com/nexssp/from-child v2.0.0\r\n"
	file := "/project/main.nflow"
	ctx := compiler.WithSourceLoader(t.Context(), compiler.SourceMap{
		"/project/child.nflow": child,
	})
	_, meta, err := compiler.Preprocess(ctx, compiler.NewDirectiveTable(Directive, includeext.Directive), root, file)
	ktest.RequireNoError(t, err)

	declarations := DirectDeclarations(meta)
	ktest.RequireEqual(t, len(declarations), 4)
	ktest.RequireEqual(t, declarations[0].Line, 1)
	ktest.RequireEqual(t, declarations[1].Line, 2)
	ktest.RequireEqual(t, declarations[2].Line, 3)
	ktest.RequireEqual(t, declarations[3].Line, 4)
	ktest.RequireEqual(t, declarations[0].Requirement.Options["id"], "root")
	ktest.RequireEqual(t, declarations[1].Requirement.Options["id"], "root")
	ktest.RequireEqual(t, declarations[0].Requirement.Alias, "ali")
	ktest.RequireEqual(t, declarations[0].Requirement.Version, "v1.2.3")

	for _, i := range []int{0, 1} {
		line := strings.Split(root, "\n")[i]
		decl := declarations[i]
		ktest.RequireEqual(t, line[decl.SelectorStart:decl.SelectorEnd], "v1.2.3")
	}
	thirdLine := strings.Split(root, "\n")[2]
	ktest.RequireEqual(t, thirdLine[declarations[2].SelectorStart:declarations[2].SelectorEnd], "'feature/branch'")
	ktest.RequireEqual(t, declarations[2].Requirement.Version, "feature/branch")
	fourthLine := strings.Split(root, "\n")[3]
	ktest.RequireEqual(t, declarations[3].SelectorStart, declarations[3].SelectorEnd)
	ktest.RequireEqual(t, fourthLine[declarations[3].SelectorStart:], " as bare # comment\r")

	imports := compiler.SourceImports(meta)
	ktest.RequireEqual(t, len(imports), 1)
	childDeclarations := DirectDeclarations(imports[0].Source.Metadata())
	ktest.RequireEqual(t, len(childDeclarations), 1)
	ktest.RequireEqual(t, childDeclarations[0].Requirement.Import, "github.com/nexssp/from-child")
}

func TestDirectDeclarationsDoNotTreatCommentMarkersInsideTokensAsComments(t *testing.T) {
	t.Parallel()

	line := `@require github.com/example//nested "release#candidate" # trailing comment`
	_, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), line, "<test>")
	ktest.RequireNoError(t, err)
	declarations := DirectDeclarations(meta)
	ktest.RequireEqual(t, len(declarations), 1)
	ktest.RequireEqual(t, declarations[0].Requirement.Import, "github.com/example//nested")
	ktest.RequireEqual(t, declarations[0].Requirement.Version, "release#candidate")
	ktest.RequireEqual(t, line[declarations[0].SelectorStart:declarations[0].SelectorEnd], `"release#candidate"`)
}
