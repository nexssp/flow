package compiler

import (
	"fmt"
	"maps"

	"github.com/nexssp/kernel/xerr"
)

// MetadataMerger describes metadata keys owned by one directive. Merge receives
// the working parent map (a shallow copy of DirectiveReq.Out) and metadata from
// the included source. A merger that changes nested values reachable from the
// parent map must use copy-on-write itself; only the top-level map is copied.
// AfterMerge runs for an active merger after all merges have completed and sees
// req.Out set to the temporary working map. A failed merge or hook does not
// commit that map to the original Out map. Hooks may nevertheless mutate other
// referenced data, such as req.Lines; such side effects are not rolled back.
type MetadataMerger struct {
	Keys       []string
	Merge      func(parent, included map[string]any) error
	AfterMerge func(DirectiveReq) error
}

// MergeMetadata applies metadata from an included source transactionally to
// req.Out. Merger-owned keys are handled only by their merger, activated when
// at least one owned key exists in included. Other keys use parent-wins shallow
// copying: included values are copied only when the parent has no such key.
// The nil table behaves like a table with no mergers.
func (t *DirectiveTable) MergeMetadata(req DirectiveReq, included map[string]any) error {
	if req.Out == nil {
		return xerr.Internal("metadata merge requires a non-nil output map")
	}

	working := make(map[string]any, len(req.Out)+len(included))
	maps.Copy(working, req.Out)

	claimed := make(map[string]struct{})
	active := make([]*MetadataMerger, 0)
	if t != nil {
		for _, merger := range t.metadata {
			if merger == nil { // Tables built by NewDirectiveTable never contain nil entries.
				continue
			}
			hasIncludedKey := false
			for _, key := range merger.Keys {
				claimed[key] = struct{}{}
				if _, ok := included[key]; ok {
					hasIncludedKey = true
				}
			}
			if hasIncludedKey {
				active = append(active, merger)
			}
		}
	}

	for _, merger := range active {
		if err := merger.Merge(working, included); err != nil {
			return err
		}
	}
	for key, value := range included {
		if _, owned := claimed[key]; owned {
			continue
		}
		if _, exists := working[key]; !exists {
			working[key] = value
		}
	}
	for _, merger := range active {
		if merger.AfterMerge != nil {
			hookReq := req
			hookReq.Out = working
			if err := merger.AfterMerge(hookReq); err != nil {
				return err
			}
		}
	}

	clear(req.Out)
	maps.Copy(req.Out, working)
	return nil
}

// AppendMetadata returns a merger that appends a typed slice from included to
// the parent's typed slice under key. Values present at key must be assignable
// to []T; mismatches are returned as contextual errors. Both slices are copied
// before append so their existing backing arrays are not changed.
func AppendMetadata[T any](key string) *MetadataMerger {
	return &MetadataMerger{
		Keys: []string{key},
		Merge: func(parent, included map[string]any) error {
			var parentValues, includedValues []T
			if value, exists := parent[key]; exists {
				var ok bool
				parentValues, ok = value.([]T)
				if !ok {
					return fmt.Errorf("metadata %q: expected %T in parent, got %T", key, []T(nil), value)
				}
			}
			if value, exists := included[key]; exists {
				var ok bool
				includedValues, ok = value.([]T)
				if !ok {
					return fmt.Errorf("metadata %q: expected %T in included metadata, got %T", key, []T(nil), value)
				}
			}

			merged := make([]T, len(parentValues), len(parentValues)+len(includedValues))
			copy(merged, parentValues)
			includedCopy := make([]T, len(includedValues))
			copy(includedCopy, includedValues)
			parent[key] = append(merged, includedCopy...)
			return nil
		},
	}
}
