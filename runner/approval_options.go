package runner

import (
	"strings"

	"github.com/nexssp/kernel/xerr"
)

// ParseApprovalOption removes at most one --approval token from args and
// returns the remaining arguments unchanged and in order.
func ParseApprovalOption(args []string) (token string, remaining []string, err error) {
	remaining = make([]string, 0, len(args))
	seen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg != "--approval" && !strings.HasPrefix(arg, "--approval=") {
			remaining = append(remaining, arg)
			continue
		}
		if seen {
			return "", nil, xerr.BadRequest("--approval may be specified only once")
		}
		seen = true
		var value string
		if arg == "--approval" {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return "", nil, xerr.BadRequest("--approval requires a token")
			}
			i++
			value = args[i]
		} else {
			value = strings.TrimPrefix(arg, "--approval=")
		}
		if strings.TrimSpace(value) == "" {
			return "", nil, xerr.BadRequest("--approval value cannot be empty")
		}
		token = value
	}
	return token, remaining, nil
}
