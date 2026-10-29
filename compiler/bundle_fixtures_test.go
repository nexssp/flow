package compiler

import (
	"testing"
	"testing/fstest"
)

func TestDiscoverFixtures_LoadsCompanionFilesFromPerFixtureDirectory(t *testing.T) {
	t.Parallel()

	fixtures := fstest.MapFS{
		"nflows/happy/read.nflow":       &fstest.MapFile{Data: []byte("@description \"Happy: read a local fixture\"\nnoop")},
		"nflows/happy/read/input.txt":   &fstest.MapFile{Data: []byte("example input\n")},
		"nflows/happy/read/child.nflow": &fstest.MapFile{Data: []byte("@pipeline child\n  noop\n@end\n")},
		"nflows/edge/no_data.nflow":     &fstest.MapFile{Data: []byte("@description \"Edge: no sidecar directory\"\nnoop")},
	}

	features := discoverFixtures("sample", fixtures)
	if len(features) != 2 {
		t.Fatalf("discovered %d fixtures, want 2", len(features))
	}
	var read *SelfTestFeature
	for i := range features {
		if features[i].Name == "Happy: read a local fixture" {
			read = &features[i]
		}
	}
	if read == nil {
		t.Fatal("fixture with companion files was not discovered")
	}
	if got := read.Files["input.txt"]; got != "example input\n" {
		t.Fatalf("companion data = %q, want %q", got, "example input\n")
	}
	if got := read.Files["child.nflow"]; got == "" {
		t.Fatal("nested .nflow companion was not included as parent fixture data")
	}
	for i := range features {
		if features[i].Name == "Edge: no sidecar directory" && len(features[i].Files) != 0 {
			t.Fatalf("fixture without companion directory has data: %#v", features[i].Files)
		}
	}
}

func TestDiscoverFixtures_LoadsFlatCompanionFiles(t *testing.T) {
	t.Parallel()

	fixtures := fstest.MapFS{
		"nflows/read.nflow":                  &fstest.MapFile{Data: []byte("@description \"Read flat inputs\"\nnoop")},
		"nflows/read--input.txt":             &fstest.MapFile{Data: []byte("example input\n")},
		"nflows/read--schemas--settings.yml": &fstest.MapFile{Data: []byte("host: example.test\n")},
		"nflows/read--child.nflow":           &fstest.MapFile{Data: []byte("@pipeline child\n  noop\n@end\n")},
		"nflows/no_data.nflow":               &fstest.MapFile{Data: []byte("@description \"No sidecars\"\nnoop")},
	}

	features := discoverFixtures("sample", fixtures)
	if len(features) != 2 {
		t.Fatalf("discovered %d fixtures, want 2", len(features))
	}
	var read *SelfTestFeature
	for i := range features {
		if features[i].Name == "Read flat inputs" {
			read = &features[i]
		}
	}
	if read == nil {
		t.Fatal("fixture with flat companion files was not discovered")
	}
	if got := read.Files["input.txt"]; got != "example input\n" {
		t.Fatalf("flat companion data = %q, want %q", got, "example input\n")
	}
	if got := read.Files["schemas/settings.yml"]; got != "host: example.test\n" {
		t.Fatalf("nested flat companion data = %q, want %q", got, "host: example.test\n")
	}
	if got := read.Files["child.nflow"]; got == "" {
		t.Fatal("flat child flow was not included as parent fixture data")
	}
	for _, feature := range features {
		if feature.Name == "No sidecars" && len(feature.Files) != 0 {
			t.Fatalf("fixture without sidecars has data: %#v", feature.Files)
		}
	}
}

func TestDiscoverFixturesRecognizesSyntaxOnlyMarker(t *testing.T) {
	t.Parallel()

	fixtures := fstest.MapFS{
		"nflows/syntax.nflow":  &fstest.MapFile{Data: []byte("@description \"Syntax fixture\"\n# @syntax-only\nnoop")},
		"nflows/runtime.nflow": &fstest.MapFile{Data: []byte("@description \"Runtime fixture\"\nnoop")},
	}

	features := discoverFixtures("sample", fixtures)
	if len(features) != 2 {
		t.Fatalf("discovered %d fixtures, want 2", len(features))
	}
	for _, feature := range features {
		switch feature.Name {
		case "Syntax fixture":
			if !feature.SyntaxOnly {
				t.Errorf("syntax fixture SyntaxOnly = false, want true")
			}
		case "Runtime fixture":
			if feature.SyntaxOnly {
				t.Errorf("ordinary fixture SyntaxOnly = true, want false")
			}
		default:
			t.Errorf("unexpected fixture %q", feature.Name)
		}
	}
}
