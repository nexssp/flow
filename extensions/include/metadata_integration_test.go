package include

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/approval"
	assertext "github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/hook"
)

func writeMetadataFixture(t *testing.T, root, name, source string) {
	t.Helper()
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(root, name), []byte(source), 0o600))
}

func TestIncludeKeepsCustomExtensionDeclarationsLocal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeMetadataFixture(t, root, "first.nflow", "@custom shared\n")
	writeMetadataFixture(t, root, "second.nflow", "@custom shared\n")
	custom := compiler.Directive{
		Name: "custom",
		Handler: func(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
			fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(req.Lines[req.I], "@custom")))
			if len(fields) != 1 {
				return compiler.DirectiveRes{}, compiler.SourceError(compiler.Position{File: req.File, Line: req.I + 1}, "@custom requires one declaration name")
			}
			if err := req.Declare("custom", fields[0]); err != nil {
				return compiler.DirectiveRes{}, err
			}
			existing, _ := req.Out["custom.values"].([]string)
			req.Out["custom.values"] = append(existing, fields[0])
			return compiler.DirectiveRes{Next: req.I + 1}, nil
		},
	}
	dt := compiler.NewDirectiveTable(Directive, custom)
	_, meta, err := compiler.Preprocess(t.Context(), dt, "@custom root\n@include first.nflow\n@include second.nflow", filepath.Join(root, "main.nflow"))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, meta["custom.values"], any([]string{"root"}))

	imports := compiler.SourceImports(meta)
	ktest.RequireEqual(t, len(imports), 2)
	for _, imported := range imports {
		childMeta := imported.Source.Metadata()
		ktest.RequireEqual(t, childMeta["custom.values"], any([]string{"shared"}))
		pos, ok := compiler.DeclarationPosition(childMeta, "custom", "shared")
		ktest.RequireTrue(t, ok)
		name, _ := imported.Source.SourceIdentity()
		ktest.RequireEqual(t, filepath.Base(pos.File), filepath.Base(name))
	}
}

func TestIncludePreservesWhitespaceAndCommentBoundariesWithoutEmbeddingChild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	child := "# child comment\n{value: \"two  spaces\"}\n"
	writeMetadataFixture(t, root, "child.nflow", child)
	clean, metadata, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), "@include child.nflow\n-> runtime.noop", filepath.Join(root, "main.nflow"))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, clean, "child.__source__\n-> runtime.noop")
	ktest.RequireFalse(t, strings.Contains(clean, "two  spaces"))
	imports := compiler.SourceImports(metadata)
	ktest.RequireEqual(t, len(imports), 1)
	name, _ := imports[0].Source.SourceIdentity()
	ktest.RequireEqual(t, filepath.Base(name), "child.nflow")
}

func TestIncludeKeepsApprovalAssertionAndHookDeclarationsInTheirSourceModules(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeMetadataFixture(t, root, "first.nflow", "@approval first {\nrole: reviewer\nreason: first approval\n}\n@assert result != nil\n@hook hook.first\n")
	writeMetadataFixture(t, root, "second.nflow", "@approval second {\nrole: reviewer\nreason: second approval\n}\n@assert result.value == 1\n@hook hook.second\n")
	dt := compiler.NewDirectiveTable(Directive, approval.Directive, assertext.Directive, hook.Directive)
	_, meta, err := compiler.Preprocess(t.Context(), dt, "@include first.nflow\n@include second.nflow", filepath.Join(root, "main.nflow"))
	ktest.RequireNoError(t, err)
	_, rootHasApprovals := meta["approvals"]
	_, rootHasAsserts := meta["asserts"]
	_, rootHasHooks := meta["hooks"]
	ktest.RequireFalse(t, rootHasApprovals)
	ktest.RequireFalse(t, rootHasAsserts)
	ktest.RequireFalse(t, rootHasHooks)

	imports := compiler.SourceImports(meta)
	ktest.RequireEqual(t, len(imports), 2)
	for i, expected := range []struct {
		name, assertion, hook string
	}{{"first", "result != nil", "hook.first"}, {"second", "result.value == 1", "hook.second"}} {
		childMeta := imports[i].Source.Metadata()
		declarations := approval.DeclarationsFromMeta(childMeta)
		ktest.RequireEqual(t, len(declarations), 1)
		ktest.RequireEqual(t, declarations[0].Name, expected.name)
		ktest.RequireEqual(t, childMeta["asserts"], any([]string{expected.assertion}))
		ktest.RequireEqual(t, childMeta["hooks"], any([]string{expected.hook}))
	}
}

func TestIncludeAllowsSameCustomDeclarationNameInSeparateSourceModules(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	declaration := "@approval review {\nrole: reviewer\nreason: confirm\n}\n"
	writeMetadataFixture(t, root, "first.nflow", declaration)
	writeMetadataFixture(t, root, "second.nflow", declaration)
	dt := compiler.NewDirectiveTable(Directive, approval.Directive)
	_, meta, err := compiler.Preprocess(t.Context(), dt, "@include first.nflow\n@include second.nflow", filepath.Join(root, "main.nflow"))
	ktest.RequireNoError(t, err)
	imports := compiler.SourceImports(meta)
	ktest.RequireEqual(t, len(imports), 2)
	for _, imported := range imports {
		declarations := approval.DeclarationsFromMeta(imported.Source.Metadata())
		ktest.RequireEqual(t, len(declarations), 1)
		ktest.RequireEqual(t, declarations[0].Name, "review")
	}
}

func TestIncludeRejectsDuplicateCustomDeclarationWithinOneSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeMetadataFixture(t, root, "child.nflow", "@custom repeated\n@custom repeated\n")
	custom := compiler.Directive{
		Name: "custom",
		Handler: func(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
			name := strings.TrimSpace(strings.TrimPrefix(req.Lines[req.I], "@custom"))
			if err := req.Declare("custom", name); err != nil {
				return compiler.DirectiveRes{}, err
			}
			return compiler.DirectiveRes{Next: req.I + 1}, nil
		},
	}
	dt := compiler.NewDirectiveTable(Directive, custom)
	_, _, err := compiler.Preprocess(t.Context(), dt, "@include child.nflow", filepath.Join(root, "main.nflow"))
	ktest.RequireErrorContains(t, err, "duplicate declaration @custom \"repeated\"")
	ktest.RequireErrorContains(t, err, "first declared at")
	ktest.RequireErrorContains(t, err, "child.nflow:1")
	ktest.RequireErrorContains(t, err, "child.nflow:2")
}
