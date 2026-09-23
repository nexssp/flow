package directives

import "strings"

type atProfile struct{}

func init() { Register(atProfile{}) }

func (atProfile) Name() string { return "profile" }

// Syntax:
//
//	@profile: trusted
//	@profile: untrusted_input
//	@profile: network_isolated
//
// Validation is delegated to ctx.ValidateProfile, which flow.Preprocess
// injects. A nil callback disables validation (parser-only tests).
func (atProfile) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	raw, ok := StripDirectivePrefix(line, "profile")
	if !ok {
		return 0, AtErr(ctx, i, "profile", "malformed directive")
	}
	raw = TrimQuotes(raw)

	if raw == "" {
		return 0, AtErr(ctx, i, "profile", "requires a name")
	}

	if ctx.ValidateProfile != nil {
		if err := ctx.ValidateProfile(raw); err != nil {
			return 0, AtErr(ctx, i, "profile", err.Error())
		}
	}

	if ctx.Out.Profile != "" && ctx.Out.Profile != raw {
		return 0, AtErrf(ctx, i, "profile",
			"conflict with earlier declaration: %q vs %q",
			ctx.Out.Profile, raw)
	}

	ctx.Out.Profile = raw
	ctx.Out.ProfilePos = Position{File: ctx.File, Line: i + 1}

	return i + 1, nil
}

// mergeProfile combines two profile names, tolerating one being empty.
func mergeProfile(current, incoming, source string) (string, error) {
	if incoming == "" {
		return current, nil
	}
	if current == "" {
		return incoming, nil
	}
	if current != incoming {
		return "", errf("profile conflict: %q vs %q (%s)", current, incoming, source)
	}
	return current, nil
}
