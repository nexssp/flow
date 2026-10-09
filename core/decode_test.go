package core_test

import (
	"testing"

	"github.com/nexssp/flow/core"
)

func TestDecode_NflowTag(t *testing.T) {
	type cfg struct {
		Addr string `nflow:"addr" default:":8090"`
		Name string `nflow:"name"`
	}
	got, err := core.Decode[cfg](map[string]string{"name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Addr != ":8090" || got.Name != "x" {
		t.Fatalf("tags not read: %+v", got)
	}
}
