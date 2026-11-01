package pipeline

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestSplitSchemaModifiersSharedAlias(t *testing.T) {
	request, response, compileModifiers, err := splitSchemaModifiers("echo", []string{
		"schema=!Shared",
		"timeout=5s",
	})
	ktest.RequireNoError(t, err)
	want := schemaSelection{name: "Shared", strict: true}
	ktest.RequireEqual(t, request, want)
	ktest.RequireEqual(t, response, want)
	ktest.RequireEqual(t, compileModifiers, []string{"timeout=5s"})
}

func TestSplitSchemaModifiersPreservesDifferentBoundarySchemas(t *testing.T) {
	request, response, _, err := splitSchemaModifiers("echo", []string{
		"request-schema=Request",
		"response-schema=Response",
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, request, schemaSelection{name: "Request"})
	ktest.RequireEqual(t, response, schemaSelection{name: "Response"})
}

func TestSplitSchemaModifiersRejectsSharedAliasWithBoundarySpecificSchema(t *testing.T) {
	for _, boundaryModifier := range []string{"request-schema=Request", "response-schema=Response"} {
		t.Run(boundaryModifier, func(t *testing.T) {
			_, _, _, err := splitSchemaModifiers("echo", []string{"schema=Shared", boundaryModifier})
			ktest.RequireErrorContains(t, err, "cannot be combined with boundary-specific")
		})
	}
}
