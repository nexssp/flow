package constants

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/schema"
)

func TestConstLoadSchemaAppliesDefaults(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, "settings.json"), []byte(`{"name":"custom"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	meta := map[string]any{
		"schemas": []schema.Schema{{
			Name: "Settings",
			Fields: []schema.Field{
				{
					Name: "Name", JSONName: "name", Type: "string", Kind: schema.KindString,
					Tags: map[string]string{"validate": "required"},
				},
				{
					Name: "Depth", JSONName: "depth", Type: "int", Kind: schema.KindInt,
					Tags: map[string]string{"default": "4", "validate": "required"},
				},
			},
		}},
	}
	req := compiler.DirectiveReq{
		Lines:   []string{`@const.load "settings.json" as app :schema=Settings`},
		Out:     meta,
		BaseDir: baseDir,
		File:    "settings.nflow",
	}

	res, err := handleConstLoad(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Next != 1 {
		t.Fatalf("next line = %d, want 1", res.Next)
	}
	got, ok := meta["constants"].(map[string]string)
	if !ok {
		t.Fatalf("constants have type %T, want map[string]string", meta["constants"])
	}
	if got["app.name"] != "custom" || got["app.depth"] != "4" {
		t.Fatalf("constants = %#v, want name=custom and depth=4", got)
	}
}
