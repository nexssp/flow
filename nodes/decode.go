package nodes

import "github.com/nexssp/kernel/action"

// decodePayload decodes an input into target.
// It acts as a thin wrapper over the unified kernel action.Assign.
func decodePayload(input any, target any) error {
	if target == nil || input == nil {
		return nil
	}

	// O(1) Fast-path for `*any` used dynamically in pipelines
	if ptr, ok := target.(*any); ok {
		*ptr = input

		return nil
	}

	return action.Assign(target, input)
}
