package cli

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	devNull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devNull, devNull

	code := m.Run()

	os.Stdout, os.Stderr = origOut, origErr
	_ = devNull.Close()
	os.Exit(code)
}
