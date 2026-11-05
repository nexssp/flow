package terminal

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestIsTerminalRejectsNonFileWriters(t *testing.T) {
	t.Parallel()

	ktest.RequireFalse(t, IsTerminal(bytes.NewBuffer(nil)))
	ktest.RequireFalse(t, IsTerminal(io.Discard))
}

func TestIsTerminalRejectsRegularFile(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "not-a-terminal")
	ktest.RequireNoError(t, err)
	t.Cleanup(func() {
		ktest.RequireNoError(t, file.Close())
	})

	ktest.RequireFalse(t, IsTerminal(file))
}

func TestIsTerminalRejectsClosedFile(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "closed-file")
	ktest.RequireNoError(t, err)
	ktest.RequireNoError(t, file.Close())

	ktest.RequireFalse(t, IsTerminal(file))
}
