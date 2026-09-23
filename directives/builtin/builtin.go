// Package builtin holds the shipped directives, one subdirectory per
// directive. Importing this package activates every shipped directive
// through the init() chain.
package builtin

import (
	_ "github.com/nexssp/flow/directives/builtin/at_approval"
	_ "github.com/nexssp/flow/directives/builtin/at_fallback"
	_ "github.com/nexssp/flow/directives/builtin/at_on"
	_ "github.com/nexssp/flow/directives/builtin/at_on_error"
	_ "github.com/nexssp/flow/directives/builtin/at_parallel"
	_ "github.com/nexssp/flow/directives/builtin/at_race"
	_ "github.com/nexssp/flow/directives/builtin/at_retry"
	_ "github.com/nexssp/flow/directives/builtin/at_route"
	_ "github.com/nexssp/flow/directives/builtin/at_schema"
)
