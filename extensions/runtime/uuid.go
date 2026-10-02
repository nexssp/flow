package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// UUID generates a UUID v4. Without args it returns a bare string; with
// @{ as: "field" } it merges the UUID into the input map under that key.
var UUID = action.New("runtime.uuid", func(_ context.Context, in any) (any, error) {
	id, err := newUUIDv4()
	if err != nil {
		return nil, xerr.Internal("uuid: entropy source failed", err)
	}

	if key := strings.TrimSpace(readStringArg(in, "as")); key != "" {
		base := map[string]any{}
		if m, ok := in.(map[string]any); ok {
			for k, v := range m {
				if k == "as" {
					continue
				}
				base[k] = v
			}
		}
		base[key] = id
		return base, nil
	}
	return id, nil
}).Description("Generate a UUID v4; returns a string, or merges under @{ as: ... }").
	Tag("base", "runtime").
	Build()

func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:]), nil
}
