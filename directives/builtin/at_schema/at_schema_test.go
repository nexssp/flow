package at_schema_test

import (
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/directives/builtin/at_schema"
)

func TestSchema_Directive(t *testing.T) {
	t.Parallel()

	src := `
@schema User struct {
	ID    int    ` + "`json:\"id\" cli:\"id\" validate:\"required\"`" + `
	Name  string ` + "`json:\"name\" cli:\"name,r\" validate:\"required,min=1\" desc:\"Username\"`" + `
	Email string ` + "`json:\"email\"`" + `
	Tags  []string
	Meta  map[string]string
	Home  Address
	Work  *Address
}

@schema Address struct {
	Street string
	City   string
}

noop
`

	pre, err := flow.PreprocessBytes([]byte(src), "schema_test.nflow")
	if err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}

	schemas := at_schema.SchemasFromPreprocessed(pre)
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}

	user, ok := at_schema.SchemaByName(pre, "User")
	if !ok {
		t.Fatal("User schema not found")
	}
	if user.Name != "User" {
		t.Errorf("schema name = %q", user.Name)
	}
	if len(user.Fields) != 7 {
		t.Fatalf("User has %d fields, want 7", len(user.Fields))
	}

	id := user.Fields[0]
	if id.Name != "ID" || id.Kind != at_schema.KindInt {
		t.Errorf("ID field: %+v", id)
	}
	if id.Tags["validate"] != "required" {
		t.Errorf("ID validate tag = %q", id.Tags["validate"])
	}
	if id.JSONName != "id" {
		t.Errorf("ID JSON name = %q", id.JSONName)
	}

	name := user.Fields[1]
	if name.Name != "Name" || name.Kind != at_schema.KindString {
		t.Errorf("Name field: %+v", name)
	}
	if name.Tags["cli"] != "name,r" {
		t.Errorf("Name cli = %q", name.Tags["cli"])
	}
	if name.Tags["desc"] != "Username" {
		t.Errorf("Name desc = %q", name.Tags["desc"])
	}

	tags := user.Fields[3]
	if !tags.Slice || tags.ElemType != "string" {
		t.Errorf("Tags field: %+v", tags)
	}

	meta := user.Fields[4]
	if !meta.Map || meta.ElemType != "string" {
		t.Errorf("Meta field: %+v", meta)
	}

	home := user.Fields[5]
	if home.Kind != at_schema.KindStruct || home.ElemType != "Address" {
		t.Errorf("Home field: %+v", home)
	}

	work := user.Fields[6]
	if !work.Pointer || work.Kind != at_schema.KindStruct {
		t.Errorf("Work field: %+v", work)
	}

	ex := user.JSONExample()
	if ex["id"] != int64(0) || ex["name"] != "" {
		t.Errorf("JSON example unexpected: %+v", ex)
	}
}

func TestSchema_DuplicateRejected(t *testing.T) {
	t.Parallel()

	src := `
@schema User struct { ID int }
@schema User struct { Name string }
noop
`

	_, err := flow.PreprocessBytes([]byte(src), "dup.nflow")
	if err == nil {
		t.Fatal("expected duplicate schema error")
	}
}

func TestSchema_EmptyRejected(t *testing.T) {
	t.Parallel()

	src := `
@schema User struct {
}
noop
`

	_, err := flow.PreprocessBytes([]byte(src), "empty.nflow")
	if err == nil {
		t.Fatal("expected empty schema error")
	}
}

func TestSchema_BadFieldNameRejected(t *testing.T) {
	t.Parallel()

	src := `
@schema User struct {
	id int
}
noop
`

	_, err := flow.PreprocessBytes([]byte(src), "badfield.nflow")
	if err == nil {
		t.Fatal("expected unexported field error")
	}
}
