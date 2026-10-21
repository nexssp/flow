package include

import (
	"context"
	"os"
	"path/filepath"
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

func TestIncludeExtensionOwnsMetadataWithoutIncludeChanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeMetadataFixture(t, root, "first.nflow", "@custom\n")
	writeMetadataFixture(t, root, "second.nflow", "@custom\n")
	custom := compiler.Directive{
		Name:     "custom",
		Metadata: compiler.AppendMetadata[string]("custom.values"),
		Handler: func(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
			req.Out["custom.values"] = []string{filepath.Base(req.File)}
			return compiler.DirectiveRes{Next: req.I + 1}, nil
		},
	}
	dt := compiler.NewDirectiveTable(Directive, custom)
	_, meta, err := compiler.Preprocess(t.Context(), dt, "@include first.nflow\n@include second.nflow", filepath.Join(root, "main.nflow"))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, meta["custom.values"], any([]string{"first.nflow", "second.nflow"}))
}

func TestIncludePreservesWhitespaceAndCommentBoundaries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	child := "# child comment\n{value: \"two  spaces\"}\n"
	writeMetadataFixture(t, root, "child.nflow", child)
	clean, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), "@include child.nflow\n-> runtime.noop", filepath.Join(root, "main.nflow"))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, clean, child+"\n-> runtime.noop")
}

func TestIncludeKeepsAllApprovalAssertionAndHookDeclarations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeMetadataFixture(t, root, "first.nflow", "@approval first {\nrole: reviewer\nreason: first approval\n}\n@assert result != nil\n@hook hook.first\n")
	writeMetadataFixture(t, root, "second.nflow", "@approval second {\nrole: reviewer\nreason: second approval\n}\n@assert result.value == 1\n@hook hook.second\n")
	dt := compiler.NewDirectiveTable(Directive, approval.Directive, assertext.Directive, hook.Directive)
	_, meta, err := compiler.Preprocess(t.Context(), dt, "@include first.nflow\n@include second.nflow", filepath.Join(root, "main.nflow"))
	ktest.RequireNoError(t, err)
	declarations := approval.DeclarationsFromMeta(meta)
	ktest.RequireEqual(t, len(declarations), 2)
	ktest.RequireEqual(t, declarations[0].Name, "first")
	ktest.RequireEqual(t, declarations[1].Name, "second")
	ktest.RequireEqual(t, meta["asserts"], any([]string{"result != nil", "result.value == 1"}))
	ktest.RequireEqual(t, meta["hooks"], any([]string{"hook.first", "hook.second"}))
}

func TestIncludeRejectsDuplicateApprovalAcrossFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	declaration := "@approval review {\nrole: reviewer\nreason: confirm\n}\n"
	writeMetadataFixture(t, root, "first.nflow", declaration)
	writeMetadataFixture(t, root, "second.nflow", declaration)
	dt := compiler.NewDirectiveTable(Directive, approval.Directive)
	_, _, err := compiler.Preprocess(t.Context(), dt, "@include first.nflow\n@include second.nflow", filepath.Join(root, "main.nflow"))
	ktest.RequireErrorContains(t, err, "duplicate")
	ktest.RequireErrorContains(t, err, "first.nflow")
	ktest.RequireErrorContains(t, err, "second.nflow")
}
