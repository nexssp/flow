package schema

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestParseStructTags(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      string
		want    map[string]string
		wantErr bool
	}{
		{"single", `json:"name"`, map[string]string{"json": "name"}, false},
		{"two tags", `json:"a" validate:"required"`, map[string]string{"json": "a", "validate": "required"}, false},
		{"empty", ``, map[string]string{}, false},
		{"unclosed value", `json:"name`, nil, true},
		{"missing colon", `json"name"`, nil, true},
		{"missing quote", `json:name`, nil, true},
		{"duplicate", `json:"a" json:"b"`, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseStructTags(c.in)
			if c.wantErr {
				ktest.RequireCondition(t, err != nil, "expected error for %q", c.in)
				return
			}
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestIsValidSchemaName(t *testing.T) {
	t.Parallel()
	yes := []string{"User", "user", "_x", "A1", "Plan2"}
	no := []string{"", "1abc", "a-b", "a.b", "a b"}
	for _, s := range yes {
		ktest.RequireCondition(t, isValidSchemaName(s), "expected %q valid", s)
	}
	for _, s := range no {
		ktest.RequireCondition(t, !isValidSchemaName(s), "expected %q invalid", s)
	}
}

func TestClassifyFieldType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		typeStr string
		check   func(t *testing.T, f Field)
	}{
		{"string", "string", func(t *testing.T, f Field) {
			ktest.RequireEqual(t, f.Kind, KindString)
		}},
		{"int", "int64", func(t *testing.T, f Field) {
			ktest.RequireEqual(t, f.Kind, KindInt)
		}},
		{"pointer", "*User", func(t *testing.T, f Field) {
			ktest.RequireCondition(t, f.Pointer, "should be pointer")
			ktest.RequireEqual(t, f.Kind, KindStruct)
		}},
		{"slice", "[]string", func(t *testing.T, f Field) {
			ktest.RequireCondition(t, f.Slice, "should be slice")
			ktest.RequireEqual(t, f.ElemType, "string")
		}},
		{"map", "map[string]int", func(t *testing.T, f Field) {
			ktest.RequireCondition(t, f.Map, "should be map")
			ktest.RequireEqual(t, f.ElemType, "int")
			ktest.RequireEqual(t, f.Kind, KindInt)
		}},
		{"any", "any", func(t *testing.T, f Field) {
			ktest.RequireEqual(t, f.Kind, KindAny)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := Field{Type: c.typeStr}
			classifyFieldType(&f)
			c.check(t, f)
		})
	}
}

func TestLowerFirst(t *testing.T) {
	t.Parallel()
	ktest.RequireEqual(t, lowerFirst("User"), "user")
	ktest.RequireEqual(t, lowerFirst("user"), "user")
	ktest.RequireEqual(t, lowerFirst(""), "")
	ktest.RequireEqual(t, lowerFirst("ABC"), "aBC")
}
