package selftest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestPrepareFeatureDirRejectsPathTraversal(t *testing.T) {
	outside := filepath.Join(os.TempDir(), filepath.Base(t.TempDir())+"-escape")
	feature := compiler.SelfTestFeature{Files: map[string]string{"../" + filepath.Base(outside): "must not be written"}}

	cleanup, err := prepareFeatureDir(feature)
	if err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("prepareFeatureDir accepted a path outside its temporary directory")
	}
	if _, statErr := os.Stat(outside); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside fixture unexpectedly exists: %v", statErr)
	}
}

func TestPrepareFeatureDirReturnsMkdirError(t *testing.T) {
	feature := compiler.SelfTestFeature{Files: map[string]string{"\x00/child": "content"}}
	cleanup, err := prepareFeatureDir(feature)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("prepareFeatureDir returned no error when fixture directory creation failed")
	}
}
