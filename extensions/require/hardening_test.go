package require

import (
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestDuplicateOptionKeysAreRejected(t *testing.T) {
	for _, test := range []struct {
		name  string
		lines []string
	}{
		{
			name:  "inline",
			lines: []string{`@require example.com/foo { id: first, id: second }`},
		},
		{
			name:  "block",
			lines: []string{`@require example.com/foo`, `{`, `id: first`, `id: second`, `}`},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := runDirective(t, test.lines...)
			if err == nil || !strings.Contains(err.Error(), `duplicate option "id"`) {
				t.Fatalf("duplicate option error = %v, want explicit duplicate option error", err)
			}
		})
	}
}

func TestConflictingDuplicateRequirementReportsBothSourcePositions(t *testing.T) {
	file := "main.nflow"
	source := "@require example.com/foo v1.0.0 as foo { id: first }\n@require example.com/foo v1.0.0 as foo { id: second }"
	_, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), source, file)
	if err == nil {
		t.Fatal("conflicting duplicate @require options unexpectedly succeeded")
	}
	for _, want := range []string{
		"main.nflow:2",
		"first declared at main.nflow:1",
		"conflicting options",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("duplicate @require error %q does not include %q", err, want)
		}
	}
}

func TestIdenticalDuplicateRequirementsRemainIdempotent(t *testing.T) {
	file := "main.nflow"
	source := "@require example.com/foo v1.0.0 as foo { id: same }\n@require example.com/foo v1.0.0 as foo { id: same }"
	_, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), source, file)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := SourceRequirements(meta)
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 1 || requirements[0].Options["id"] != "same" {
		t.Fatalf("identical duplicate requirements = %#v, want one preserved requirement", requirements)
	}
}

func TestAliasConflictReportsFirstAndSecondSourcePositions(t *testing.T) {
	source := "@require example.com/first as shared\n\n@require example.com/second as shared"
	_, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), source, "main.nflow")
	if err == nil {
		t.Fatal("duplicate @require aliases unexpectedly succeeded")
	}
	for _, want := range []string{"main.nflow:3", "first declared at main.nflow:1", `namespace qualifier "shared"`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("alias conflict error %q does not include %q", err, want)
		}
	}
}

func TestFromMetaPropagatesInvalidRequirementMetadata(t *testing.T) {
	requirements, err := FromMeta(map[string]any{"require": "not a requirement slice"})
	if err == nil {
		t.Fatal("FromMeta silently accepted malformed requirement metadata")
	}
	if len(requirements) != 0 {
		t.Fatalf("requirements = %#v, want no partial result on metadata error", requirements)
	}
}

func TestFromMetaRejectsEmptyRequirementTargets(t *testing.T) {
	_, err := FromMeta(map[string]any{"require": []Requirement{{}}})
	if err == nil || !strings.Contains(err.Error(), "empty import target") {
		t.Fatalf("empty requirement target error = %v, want explicit metadata error", err)
	}
}
