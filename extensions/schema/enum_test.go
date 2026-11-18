package schema

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	symbolsext "github.com/nexssp/flow/extensions/symbols"
)

func preprocessEnumSchemas(source string) (map[string]any, error) {
	lines := strings.Split(source, "\n")
	meta := make(map[string]any)
	for index := 0; index < len(lines); {
		trimmed := strings.TrimSpace(lines[index])
		switch {
		case strings.HasPrefix(trimmed, "@enum"):
			result, err := handleEnumDirective(context.Background(), compiler.DirectiveReq{
				Lines: lines, I: index, Out: meta, File: "enum-test.nflow",
			})
			if err != nil {
				return meta, err
			}
			index = result.Next
		case strings.HasPrefix(trimmed, "@schema"):
			result, err := handleDirective(context.Background(), compiler.DirectiveReq{
				Lines: lines, I: index, Out: meta, File: "enum-test.nflow",
			})
			if err != nil {
				return meta, err
			}
			index = result.Next
		case strings.HasPrefix(trimmed, "@symbols"):
			result, err := symbolsext.Directive.Handler(context.Background(), compiler.DirectiveReq{
				Lines: lines, I: index, Out: meta, File: "enum-test.nflow",
			})
			if err != nil {
				return meta, err
			}
			index = result.Next
		default:
			index++
		}
	}
	return meta, nil
}

func TestEnumSchemaDefaultsMembershipAndJSONSchema(t *testing.T) {
	t.Parallel()
	meta, err := preprocessEnumSchemas(`@enum OrderStatus {
  pending
  paid = "settled"
  canceled = "canceled_by_user"
}
@schema Order {
  status OrderStatus ` + "`json:\"status\" default:\"OrderStatus.pending\"`" + `
  refund_status OrderStatus ` + "`json:\"refund_status\" validate:\"required,oneof=OrderStatus.paid OrderStatus.canceled\"`" + `
  optional_status OrderStatus
}`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(EnumsFromMap(meta)), 1)
	ktest.RequireEqual(t, len(SchemasFromMap(meta)), 1)
	fields := SchemasFromMap(meta)[0].Fields
	ktest.RequireEqual(t, fields[0].EnumType, "OrderStatus")
	ktest.RequireEqual(t, fields[0].EnumValues, []string{"pending", "settled", "canceled_by_user"})
	ktest.RequireEqual(t, fields[0].Tags["default"], "pending")
	ktest.RequireEqual(t, fields[1].Tags["validate"], "required,oneof=settled canceled_by_user")

	effective, err := ApplyDefaultsAndValidate(t.Context(), SchemasFromMap(meta)[0], map[string]any{
		"refund_status": "settled",
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual[any](t, effective["status"], "pending")
	ktest.RequireEqual[any](t, effective["refund_status"], "settled")
	if _, exists := effective["optional_status"]; exists {
		t.Fatalf("optional_status = %#v, want no inferred enum default", effective["optional_status"])
	}

	_, err = ApplyDefaultsAndValidate(t.Context(), SchemasFromMap(meta)[0], map[string]any{
		"status":        "not-a-wire-value",
		"refund_status": "settled",
	})
	ktest.RequireErrorContains(t, err, `field "status"`)
	ktest.RequireErrorContains(t, err, "must be one of")

	_, err = ApplyDefaultsAndValidate(t.Context(), SchemasFromMap(meta)[0], map[string]any{
		"status":        "pending",
		"refund_status": "pending",
	})
	ktest.RequireErrorContains(t, err, `field "refund_status"`)

	properties, ok := ToJSONSchema(SchemasFromMap(meta)[0])["properties"].(map[string]any)
	ktest.RequireTrue(t, ok)
	status, ok := properties["status"].(map[string]any)
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual[any](t, status["enum"], []string{"pending", "settled", "canceled_by_user"})
}

func TestEnumSliceMembershipAndJSONSchema(t *testing.T) {
	t.Parallel()
	meta, err := preprocessEnumSchemas(`@enum Mode { fast, careful = "cautious" }
	@schema Request {
	  modes []Mode ` + "`json:\"modes\" validate:\"required,oneof=Mode.fast\"`" + `
	}`)
	ktest.RequireNoError(t, err)
	schema := SchemasFromMap(meta)[0]
	ktest.RequireEqual(t, schema.Fields[0].Tags["validate"], "required,dive,oneof=fast")
	_, err = ApplyDefaultsAndValidate(t.Context(), schema, map[string]any{
		"modes": []string{"fast"},
	})
	ktest.RequireNoError(t, err)
	_, err = ApplyDefaultsAndValidate(t.Context(), schema, map[string]any{
		"modes": []string{"fast", "unknown"},
	})
	ktest.RequireErrorContains(t, err, "element 1 must be one of")
	_, err = ApplyDefaultsAndValidate(t.Context(), schema, map[string]any{
		"modes": []string{"cautious"},
	})
	if err == nil {
		t.Fatal("modes accepted an enum member excluded by oneof")
	}

	properties, ok := ToJSONSchema(schema)["properties"].(map[string]any)
	if !ok {
		t.Fatal("JSON Schema properties are not an object")
	}
	modes, ok := properties["modes"].(map[string]any)
	if !ok {
		t.Fatal("JSON Schema modes property is not an object")
	}
	items, ok := modes["items"].(map[string]any)
	if !ok {
		t.Fatal("JSON Schema modes items are not an object")
	}
	ktest.RequireEqual[any](t, items["enum"], []string{"fast", "cautious"})
}

func TestEnumWireValueWithDotRemainsLiteralDuringSymbolResolution(t *testing.T) {
	t.Parallel()
	meta, err := preprocessEnumSchemas(`@symbols x { y: "other" }
@enum DotStatus { dotted = "x.y" }
@schema Request {
  value DotStatus ` + "`default:\"DotStatus.dotted\" validate:\"oneof=DotStatus.dotted\"`" + `
}`)
	ktest.RequireNoError(t, err)
	field := SchemasFromMap(meta)[0].Fields[0]
	ktest.RequireEqual(t, field.Tags["default"], "x.y")
	ktest.RequireEqual(t, field.Tags["validate"], "oneof=x.y")
	effective, err := ApplyDefaultsAndValidate(t.Context(), SchemasFromMap(meta)[0], map[string]any{})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual[any](t, effective["value"], "x.y")
}

func TestEnumRejectsInvalidWireValues(t *testing.T) {
	t.Parallel()
	duplicateMember := "same"
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"empty enum", `@enum State { }`, "at least one member"},
		{"empty wire value", `@enum State { empty = "" }`, "must not be empty"},
		{"whitespace wire value", `@enum State { spaced = "two words" }`, "must not contain whitespace"},
		{"duplicate member", strings.Join([]string{"@enum State {", " " + duplicateMember, " " + duplicateMember, "}"}, "\n"), "duplicate member"},
		{"duplicate wire value", "@enum State {\n first = \"same\"\n second = \"same\"\n}", "duplicate wire value"},
		{"unquoted override", `@enum State { active = yes }`, "expected `member = \"wire-value\"`"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := preprocessEnumSchemas(test.src)
			ktest.RequireErrorContains(t, err, test.want)
		})
	}
}

func TestEnumDefaultsAndOneofRequireMembersOfTheExactEnum(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tag  string
		want string
	}{
		{"literal default", `default:"pending"`, "must reference a member"},
		{"unknown default member", `default:"OrderStatus.unknown"`, "unknown member"},
		{"cross-enum default", `default:"PaymentStatus.paid"`, "not \"OrderStatus\""},
		{"unknown oneof member", `validate:"oneof=OrderStatus.unknown"`, "unknown member"},
		{"cross-enum oneof member", `validate:"oneof=PaymentStatus.paid"`, "not \"OrderStatus\""},
		{"bare oneof value", `validate:"oneof=pending"`, "must reference a member"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := `@enum OrderStatus { pending, paid }
@enum PaymentStatus { paid, failed }
@schema Request {
  Status OrderStatus ` + "`" + test.tag + "`" + `
}`
			_, err := preprocessEnumSchemas(source)
			ktest.RequireErrorContains(t, err, test.want)
		})
	}
}

func TestEnumNamespacesShareLocalSymbolSpaceAndSchemaTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"duplicate enum", "@enum State {\n one\n}\n@enum State {\n two\n}", "duplicate declaration"},
		{"symbols collision", "@symbols State { one }\n@enum State {\n one\n}", "duplicate declaration"},
		{"schema collision", "@enum State {\n one\n}\n@schema State {\n Value string\n}", "name conflicts with an enum"},
		{"enum declared after schema field", "@schema Request {\n Status State\n}\n@enum State {\n one\n}", "declared after @schema Request field \"Status\""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := preprocessEnumSchemas(test.src)
			ktest.RequireErrorContains(t, err, test.want)
		})
	}
}
