package nodes

import "github.com/nexssp/kernel/action"

// AssignPayload decodes an input into target. It is the standard decode
// target used by every action that needs to feed a value into another
// action's request slot. Fast path for *any is deliberate: pipeline
// nodes almost always decode into `any`, and the type assertion avoids
// the reflect path in action.Assign.
func AssignPayload(input any, target any) error {
	if target == nil || input == nil {
		return nil
	}
	if ptr, ok := target.(*any); ok {
		*ptr = input
		return nil
	}
	return action.Assign(target, input)
}
