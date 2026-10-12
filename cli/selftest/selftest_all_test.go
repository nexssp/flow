package selftest_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/nexssp/flow/cli/selftest"
)

func TestAllNFlowConformance(t *testing.T) {
	var out bytes.Buffer
	code := selftest.Run(context.Background(), selftest.Options{
		Out:     &out,
		NoColor: true,
	})
	if code != 0 {
		t.Fatalf("NFlow conformance self-test failed (exit %d):\n%s", code, out.String())
	}
}
