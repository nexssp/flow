package external

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

// TestErrorFromStatus covers the status→kind mapping in isolation.
// The HTTP round-trip itself is httptest territory and would duplicate
// net/http's own tests.
func TestErrorFromStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status int
		want   xerr.Kind
	}{
		{http.StatusBadRequest, xerr.KindBadRequest},
		{http.StatusUnauthorized, xerr.KindUnauthorized},
		{http.StatusForbidden, xerr.KindForbidden},
		{http.StatusNotFound, xerr.KindNotFound},
		{http.StatusMethodNotAllowed, xerr.KindMethodNotAllowed},
		{http.StatusConflict, xerr.KindConflict},
		{http.StatusTooManyRequests, xerr.KindTooManyRequests},
		{http.StatusRequestTimeout, xerr.KindTimeout},
		{http.StatusGatewayTimeout, xerr.KindTimeout},
		{http.StatusBadGateway, xerr.KindUnavailable},
		{http.StatusServiceUnavailable, xerr.KindUnavailable},
		{http.StatusInternalServerError, xerr.KindInternal},
		{http.StatusTeapot, xerr.KindBadRequest}, // 4xx unknown → BadRequest
	}
	for _, c := range cases {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			t.Parallel()
			err := errorFromStatus(c.status, "GET", "https://x")
			ktest.RequireErrorKind(t, err, c.want)
		})
	}
}

func TestBuildBody(t *testing.T) {
	t.Parallel()
	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		r, err := buildBody(nil)
		ktest.RequireNoError(t, err)
		// http.NoBody, not nil: buildBody returns a shared sentinel
		// reader for the empty case so net/http can short-circuit.
		ktest.RequireCondition(t, r == http.NoBody,
			"buildBody(nil) = %T, want http.NoBody", r)
	})
	t.Run("string", func(t *testing.T) {
		t.Parallel()
		r, err := buildBody("hello")
		ktest.RequireNoError(t, err)
		buf := make([]byte, 5)
		_, _ = r.Read(buf)
		ktest.RequireEqual(t, string(buf), "hello")
	})
	t.Run("map", func(t *testing.T) {
		t.Parallel()
		r, err := buildBody(map[string]any{"a": 1})
		ktest.RequireNoError(t, err)
		buf := make([]byte, 7)
		_, _ = r.Read(buf)
		ktest.RequireEqual(t, string(buf), `{"a":1}`)
	})
}

func TestDecodeBody(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		want any
	}{
		{"json object", []byte(`{"a":1}`), map[string]any{"a": float64(1)}},
		{"json array", []byte(`[1,2]`), []any{float64(1), float64(2)}},
		{"plain text", []byte("hello"), "hello"},
		{"empty", []byte(""), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, decodeBody(c.in), c.want)
		})
	}
}

func TestHTTPRequestResponseBodyLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		body     string
		tooLarge bool
	}{
		{name: "oversized text", body: strings.Repeat("x", httpResponseMaxBytes+1), tooLarge: true},
		{name: "oversized JSON", body: `"` + strings.Repeat("x", httpResponseMaxBytes-1) + `"`, tooLarge: true},
		{name: "exact limit", body: strings.Repeat("x", httpResponseMaxBytes)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			raw, err := action.InvokeAny(t.Context(), HTTPRequest, map[string]any{"url": server.URL})
			if tc.tooLarge {
				ktest.RequireErrorKind(t, err, xerr.KindValidation)
				ktest.RequireStringContains(t, err.Error(), "http.request: response body exceeds 16777216-byte limit")
				result, ok := raw.(HTTPResult)
				ktest.RequireCondition(t, ok, "result type = %T, want HTTPResult", raw)
				ktest.RequireEqual(t, result, HTTPResult{})
				return
			}

			ktest.RequireNoError(t, err)
			result, ok := raw.(HTTPResult)
			ktest.RequireCondition(t, ok, "result type = %T, want HTTPResult", raw)
			body, ok := result.Body.(string)
			ktest.RequireCondition(t, ok, "body type = %T, want string", result.Body)
			ktest.RequireEqual(t, len(body), httpResponseMaxBytes)
			ktest.RequireEqual(t, body, tc.body)
		})
	}
}
