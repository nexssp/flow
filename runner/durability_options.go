package runner

import (
	"strings"

	"github.com/nexssp/kernel/xerr"
)

// DurabilityOptions configure the FileJournal used for a single run and the
// execution identity to use when replaying that run.
type DurabilityOptions struct {
	JournalDir  string
	ExecutionID string
}

// ParseDurabilityOptions removes the runner's durability flags from args and
// returns their values. Both --flag=value and --flag value forms are accepted.
// Unrecognized arguments are returned, in order, for the caller to process.
func ParseDurabilityOptions(args []string) (DurabilityOptions, []string, error) {
	var options DurabilityOptions
	var seenJournalDir, seenExecutionID bool
	var pendingOption string
	remaining := make([]string, 0, len(args))

	setValue := func(name, value string) error {
		if value == "" {
			return xerr.BadRequest(name + " value cannot be empty")
		}
		switch name {
		case "--journal-dir":
			if seenJournalDir {
				return xerr.BadRequest("--journal-dir may be specified only once")
			}
			seenJournalDir = true
			options.JournalDir = value
		case "--execution-id":
			if seenExecutionID {
				return xerr.BadRequest("--execution-id may be specified only once")
			}
			seenExecutionID = true
			options.ExecutionID = value
		}
		return nil
	}

	for _, arg := range args {
		if pendingOption != "" {
			if isDurabilityOption(arg) {
				return DurabilityOptions{}, nil, xerr.BadRequest(pendingOption + " requires a value")
			}
			if err := setValue(pendingOption, arg); err != nil {
				return DurabilityOptions{}, nil, err
			}
			pendingOption = ""
			continue
		}

		name, value, hasValue, recognized := durabilityOption(arg)
		if !recognized {
			remaining = append(remaining, arg)
			continue
		}
		if !hasValue {
			pendingOption = name
			continue
		}
		if err := setValue(name, value); err != nil {
			return DurabilityOptions{}, nil, err
		}
	}
	if pendingOption != "" {
		return DurabilityOptions{}, nil, xerr.BadRequest(pendingOption + " requires a value")
	}

	return options, remaining, nil
}

func durabilityOption(arg string) (name, value string, hasValue, recognized bool) {
	for _, candidate := range []string{"--journal-dir", "--execution-id"} {
		if arg == candidate {
			return candidate, "", false, true
		}
		if after, ok := strings.CutPrefix(arg, candidate+"="); ok {
			return candidate, after, true, true
		}
	}
	return "", "", false, false
}

func isDurabilityOption(arg string) bool {
	_, _, _, ok := durabilityOption(arg)
	return ok
}
