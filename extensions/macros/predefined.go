package macros

// DefaultBuiltinMacros provides standard reusable validation and guard macros.
// In expr-lang, membership testing uses the 'in' operator rather than contains().
var DefaultBuiltinMacros = []Declaration{
	{
		Name:   "check_required",
		Params: []string{"field"},
		Body:   `assert(.$field != nil && .$field != "", "field '" + "$field" + "' is required")`,
	},
	{
		Name:   "check_choice",
		Params: []string{"field", "choices"},
		Body:   `assert(.$field in $choices, "field '" + "$field" + "' must be one of " + string($choices))`,
	},
	{
		Name:   "check_range",
		Params: []string{"field", "min", "max"},
		Body:   `assert(.$field >= $min && .$field <= $max, "field '" + "$field" + "' must be between " + string($min) + " and " + string($max))`,
	},
}
