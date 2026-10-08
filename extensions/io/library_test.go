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

	entries, err := fs.ReadDir(bundle.Fixtures, "nflows")
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, len(entries) >= 2,
		"expected at least 2 fixtures, got %d", len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nflow") {
			continue
		}
		body, err := fs.ReadFile(bundle.Fixtures, "nflows/"+entry.Name())
		ktest.RequireNoError(t, err)
		ktest.RequireStringContains(t, string(body), "@description")
		ktest.RequireStringContains(t, string(body), "io.")
	}
}
