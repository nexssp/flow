package utility

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// ─── noop ────────────────────────────────────────────────────────────────

func NewNoopAction() action.AnyAction {
	return action.New("noop", func(_ context.Context, in any) (any, error) {
		return in, nil
	}).
		Description("Pass input through unchanged").
		Tag("base", "identity").
		Build()
}

// ─── debug ───────────────────────────────────────────────────────────────

func NewDebugAction() action.AnyAction {
	return action.New("debug", func(_ context.Context, in any) (any, error) {
		label := readStringArg(in, "label")
		suffix := ""
		if label != "" {
			suffix = " " + label
		}

		b, err := json.MarshalIndent(in, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "[debug%s] <unprintable %T: %v>\n", suffix, in, err)
			return in, nil
		}
		fmt.Fprintf(os.Stderr, "[debug%s] %s\n", suffix, string(b))
		return in, nil
	}).
		Description("Print input as JSON to stderr, pass through unchanged").
		Tag("base", "debug").
		Build()
}

// ─── pick ────────────────────────────────────────────────────────────────

func NewPickAction() action.AnyAction {
	return action.New("pick", func(_ context.Context, in any) (any, error) {
		m, ok := in.(map[string]any)
		if !ok {
			return nil, xerr.BadRequest(
				"pick: input must be an object carrying the `field` arg")
		}
		field := strings.TrimSpace(readStringArg(m, "field"))
		if field == "" {
			return nil, xerr.BadRequest(
				`pick: field is required (use: pick @{ field: "user.name" })`)
		}
		v, ok := lookupDottedPath(m, field)
		if !ok {
			return nil, xerr.NotFound(fmt.Sprintf("pick: field %q not found", field))
		}
		return v, nil
	}).
		Description("Extract one dotted-path field from the input object").
		Tag("base", "shape").
		Build()
}

// ─── wrap ────────────────────────────────────────────────────────────────

func NewWrapAction() action.AnyAction {
	return action.New("wrap", func(_ context.Context, in any) (any, error) {
		m, ok := in.(map[string]any)
		if !ok {
			return nil, xerr.BadRequest(
				"wrap: input must be an object carrying the `key` arg")
		}
		key := strings.TrimSpace(readStringArg(m, "key"))
		if key == "" {
			return nil, xerr.BadRequest(
				`wrap: key is required (use: wrap @{ key: "data" })`)
		}

		// Drop the injected `key` field; wrap the rest.
		inner := make(map[string]any, len(m))
		for k, v := range m {
			if k == "key" {
				continue
			}
			inner[k] = v
		}
		return map[string]any{key: inner}, nil
	}).
		Description("Wrap the input object under a named key").
		Tag("base", "shape").
		Build()
}

// ─── const ───────────────────────────────────────────────────────────────

func NewConstAction() action.AnyAction {
	return action.New("const", func(_ context.Context, in any) (any, error) {
		// 1. Jeśli przekazano bezpośrednio string lub inną wartość skalarną, zwróć ją (smart fallback)
		if str, ok := in.(string); ok {
			return coerceLiteral(str), nil
		}

		// 2. Jeśli przekazano mapę, szukaj klucza "value" lub "val"
		m, ok := in.(map[string]any)
		if !ok {
			return nil, xerr.BadRequest(fmt.Sprintf(
				"const: expected object with 'value' key or raw scalar, got %T (%v)\n"+
					"              ├── hint      : use { value: %v } or const @{ value: %v }",
				in, in, in, in,
			))
		}

		raw, ok := m["value"]
		if !ok {
			raw, ok = m["val"]
		}

		if !ok {
			// Wyświetl dostępne klucze jeśli użytkownik popełnił literówkę (np. { val: ... })
			var keys []string
			for k := range m {
				keys = append(keys, k)
			}
			return nil, xerr.BadRequest(fmt.Sprintf(
				"const: missing required field 'value' in input payload\n"+
					"              ├── keys seen : %v\n"+
					"              ├── hint      : did you mean { name: \"const\", payload: { value: ... } }?",
				keys,
			))
		}

		return coerceLiteral(raw), nil
	}).
		Description("Return a fixed literal; numbers, bools, and JSON auto-parse").
		Tag("base", "literal").
		Build()
}

// ─── fail ────────────────────────────────────────────────────────────────

func NewFailAction() action.AnyAction {
	return action.New("fail", func(_ context.Context, in any) (any, error) {
		m, _ := in.(map[string]any)
		msg := strings.TrimSpace(readStringArg(m, "message"))
		if msg == "" {
			msg = "fail: pipeline deliberately aborted by the fail action"
		}
		kind := strings.TrimSpace(readStringArg(m, "kind"))
		return nil, failError(kind, msg)
	}).
		Description("Always return an error; kind configurable (default Internal)").
		Tag("base", "error").
		Build()
}

func failError(kind, msg string) *xerr.AppError {
	switch strings.ToLower(kind) {
	case "validation":
		return xerr.Validation(msg)
	case "badrequest", "bad_request":
		return xerr.BadRequest(msg)
	case "unauthorized":
		return xerr.Unauthorized(msg)
	case "forbidden":
		return xerr.Forbidden(msg)
	case "notfound", "not_found":
		return xerr.NotFound(msg)
	case "conflict":
		return xerr.Conflict(msg)
	case "timeout":
		return xerr.Timeout(msg)
	case "unavailable":
		return xerr.Unavailable(msg)
	case "toomanyrequests", "too_many_requests":
		return xerr.TooManyRequests(msg)
	default:
		return xerr.Internal(msg)
	}
}

// ─── env ─────────────────────────────────────────────────────────────────

func NewEnvAction() action.AnyAction {
	return action.New("env", func(_ context.Context, in any) (any, error) {
		name := ""
		required := false

		switch v := in.(type) {
		case string:
			// `SOMETHING -> env` — the pipeline already delivered the name.
			name = strings.TrimSpace(v)
		case map[string]any:
			// `env @{ name: "HOME" }` — name comes from the injection.
			name = strings.TrimSpace(readStringArg(v, "name"))
			if isTruthyArg(v, "required") {
				required = true
			}
		}

		if name == "" {
			return nil, xerr.BadRequest(
				`env: name is required (use: env @{ name: "HOME" })`)
		}

		val, ok := os.LookupEnv(name)
		if !ok {
			if required {
				return nil, xerr.NotFound(fmt.Sprintf("env: %s is not set", name))
			}
			return "", nil
		}
		return val, nil
	}).
		Description("Read an environment variable (required: true fails if unset)").
		Tag("base", "runtime").
		Build()
}

// ─── uuid ────────────────────────────────────────────────────────────────

func NewUUIDAction() action.AnyAction {
	return action.New("uuid", func(_ context.Context, in any) (any, error) {
		id, err := newUUIDv4()
		if err != nil {
			return nil, xerr.Internal("uuid: entropy source failed", err)
		}

		// If the caller asked for it, merge the UUID into the input map
		// under a chosen key. Otherwise return the UUID as a bare string.
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
	}).
		Description("Generate a UUID v4; returns a string, or merges under @{ as: ... }").
		Tag("base", "runtime").
		Build()
}

func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

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

// ─── helpers ─────────────────────────────────────────────────────────────

func readStringArg(in any, key string) string {
	m, ok := in.(map[string]any)
	if !ok {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func isTruthyArg(in map[string]any, key string) bool {
	v, ok := in[key]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(t))
		return err == nil && b
	}
	return false
}

// lookupDottedPath resolves "a.b.c" against a nested map. When the
// value stored under an intermediate key is a JSON object encoded as
// a string, it is decoded on the fly — this matches how env vars and
// HTTP payloads deliver nested data.
func lookupDottedPath(m map[string]any, path string) (any, bool) {
	if path == "" {
		return m, true
	}

	var current any = m
	remaining := path

	for remaining != "" {
		var segment string
		if dot := strings.IndexByte(remaining, '.'); dot >= 0 {
			segment = remaining[:dot]
			remaining = remaining[dot+1:]
		} else {
			segment = remaining
			remaining = ""
		}

		mm, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mm[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// coerceLiteral turns a string into its natural type when the string
// looks like JSON, a number, or a bool. Everything else is returned
// as-is. This makes `const @{ value: "42" }` produce the integer 42.
func coerceLiteral(raw any) any {
	s, ok := raw.(string)
	if !ok {
		return raw
	}

	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}

	// JSON object or array.
	if trimmed[0] == '{' || trimmed[0] == '[' {
		var out any
		if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
			return out
		}
	}

	// JSON quoted string.
	if trimmed[0] == '"' {
		var out string
		if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
			return out
		}
	}

	// Bool.
	switch trimmed {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}

	// Integer, then float.
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return f
	}

	return s
}
