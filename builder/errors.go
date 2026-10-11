package builder

import (
	"fmt"
	"os"
)

func fatalf(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	return 2
}
