package io

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestFixtures_EmbeddedAndDescribed(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)

	var found int
	err := fs.WalkDir(bundle.Fixtures, "nflows", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".nflow") {
			return nil
		}
		body, err := fs.ReadFile(bundle.Fixtures, path)
		if err != nil {
			return err
		}
		ktest.RequireStringContains(t, string(body), "@description")
		ktest.RequireStringContains(t, string(body), "io.")
		found++
		return nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, found, 2)
}
