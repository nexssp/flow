package core_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/native"
)

func TestIncludeDiamondIsNotACycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("common.nflow", "@pipeline c\n  const @{ value: \"c\" }\n@end\n")
	write("b.nflow", "@include ./common.nflow\n")
	write("c.nflow", "@include ./common.nflow\n")
	write("a.nflow", "@include ./b.nflow\n@include ./c.nflow\n")

	_, _, err := core.Preprocess(context.Background(),
		native.Directives(), "@include ./b.nflow\n@include ./c.nflow\n",
		filepath.Join(dir, "a.nflow"))
	if err != nil {
		t.Fatalf("diamond should not error, got: %v", err)
	}
}

func TestIncludeCycleIsDetected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.nflow", "@include ./b.nflow\n")
	write("b.nflow", "@include ./a.nflow\n")

	_, _, err := core.Preprocess(context.Background(),
		native.Directives(), "@include ./b.nflow\n",
		filepath.Join(dir, "a.nflow"))
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error should mention cycle, got: %v", err)
	}
}
