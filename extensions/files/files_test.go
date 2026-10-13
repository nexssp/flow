package files

import (
	"testing"

	"github.com/nexssp/kernel/xfile"
)

func TestValidatePath(t *testing.T) {
	for _, name := range []string{"image/logo.svg", "data.json"} {
		if err := ValidatePath(name); err != nil {
			t.Errorf("ValidatePath(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "../secret", "a/../secret", "/etc/passwd", `a\\b`, "a//b"} {
		if err := ValidatePath(name); err == nil {
			t.Errorf("ValidatePath(%q) unexpectedly succeeded", name)
		}
	}
}

func TestValidateMapRequiresTypedRefs(t *testing.T) {
	good := map[string]xfile.Ref{
		"img/logo.svg": {
			Provider: "static", ID: "img/logo.svg", Version: "sha256:abc",
			ExpectedSize: 3, ExpectedDigest: "sha256:abc",
		},
	}
	if err := ValidateMap(good); err != nil {
		t.Fatalf("valid typed map rejected: %v", err)
	}
	bad := map[string]xfile.Ref{
		"../logo.svg": {Provider: "static", ID: "logo.svg", Version: "v1", ExpectedDigest: "sha256:abc"},
	}
	if err := ValidateMap(bad); err == nil {
		t.Fatal("traversal path unexpectedly succeeded")
	}
	bad = map[string]xfile.Ref{
		"logo.svg": {Provider: "static", ID: "logo.svg", Version: "v1", ExpectedDigest: "deadbeef"},
	}
	if err := ValidateMap(bad); err == nil {
		t.Fatal("unprefixed digest unexpectedly succeeded")
	}
}
