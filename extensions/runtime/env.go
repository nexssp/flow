package runtime

import (
	"context"
	"os"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// Env reads an environment variable. Accepts either a bare string
// (env "HOME") or an object with name and required fields.
var Env = action.New("env", func(_ context.Context, in any) (any, error) {
	name := ""
	required := false

	switch v := in.(type) {
	case string:
		name = strings.TrimSpace(v)
	case map[string]any:
		name = strings.TrimSpace(readStringArg(v, "name"))
		required = isTruthyArg(v, "required")
	}

	if name == "" {
		return nil, xerr.BadRequest(`env: name is required (use: env @{ name: "HOME" })`)
	}

	value, ok := os.LookupEnv(name)
	if !ok {
		if required {
			return nil, xerr.NotFound("env: " + name + " is not set")
		}
		return "", nil
	}
	return value, nil
}).Description("Read an environment variable (required: true fails if unset)").
	Tag("base", "runtime").
	Build()
