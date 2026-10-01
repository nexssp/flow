package modifiers_core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// defaultKey hashes the request through canonical JSON. json.Marshal
// sorts map keys, so identical maps produce identical keys. A marshal
// failure returns "" — Cache, Dedup, and Coalesce treat that as "no
// key" and bypass the shared-state path, which is safer than caching
// on an arbitrary value.
func defaultKey(request any) string {
	if request == nil {
		return ""
	}
	data, err := json.Marshal(request)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}
