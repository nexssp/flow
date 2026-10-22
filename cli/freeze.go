package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/nexssp/flow/builder"
)

// runRequireFreeze is the one implementation behind the canonical command and
// its compatibility aliases. Freezing writes a sidecar lock and never edits
// the .nflow source.
func runRequireFreeze(ctx context.Context, inv *invocation, args []string, usage string) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s\n", usage)
		return 2
	}

	lockPath, err := builder.FreezeRequirements(ctx, args[0], inv.sourceRequiresFromFile)
	if err != nil {
		return fatalf("freeze requirements: %v", err)
	}
	fmt.Fprintf(os.Stdout, "✓ frozen requirements in %s\n", lockPath)
	return 0
}
