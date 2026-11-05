package maputil

import "strings"

// LookupPath walks a nested map by a dot-separated path. An empty path returns
// the root map; a missing key or non-map intermediate returns (nil, false).
func LookupPath(root map[string]any, path string) (any, bool) {
	if path == "" {
		return root, true
	}
	var current any = root
	remaining := path
	for remaining != "" {
		var segment string
		if dot := strings.IndexByte(remaining, '.'); dot >= 0 {
			segment, remaining = remaining[:dot], remaining[dot+1:]
		} else {
			segment, remaining = remaining, ""
		}
		values, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = values[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
