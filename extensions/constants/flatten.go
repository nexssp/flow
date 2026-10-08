package constants

import (
	"fmt"
)

func flattenJSON(prefix string, value any, out map[string]string) {
	switch val := value.(type) {
	case map[string]any:
		for k, child := range val {
			next := k
			if prefix != "" {
				next = prefix + "." + k
			}
			flattenJSON(next, child, out)
		}
	case []any:
		for i, child := range val {
			next := fmt.Sprintf("%s[%d]", prefix, i)
			flattenJSON(next, child, out)
		}
	default:
		out[prefix] = fmt.Sprintf("%v", val)
	}
}
