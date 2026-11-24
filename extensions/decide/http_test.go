package decide

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestHTTPBackendResponseBodyLimit(t *testing.T) {
	const prefix = `{"answers":{}}`
	exactLimitBody := prefix + strings.Repeat(" ", responseBodyMaxBytes-len(prefix))
	cases := []struct {
		name     string
		body     string
		tooLarge bool
	}{
		{name: "exact limit", body: exactLimitBody},
		{name: "over limit", body: exactLimitBody + " ", tooLarge: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			backend, err := NewHTTPBackend(HTTPConfig{Name: "test", Endpoint: server.URL})
			ktest.RequireNoError(t, err)
			result, err := backend.Decide(t.Context(), map[string]any{}, map[string]any{})
			if tc.tooLarge {
				ktest.RequireErrorKind(t, err, xerr.KindInternal)
				ktest.RequireStringContains(t, err.Error(), "response body exceeds 16777216-byte limit")
				return
			}
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, len(result.Answers), 0)
		})
	}
}

func TestHTTPBackendOversizedErrorResponsePreservesStatus(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		kind       xerr.Kind
	}{
		{name: "too many requests", statusCode: http.StatusTooManyRequests, kind: xerr.KindTooManyRequests},
		{name: "service unavailable", statusCode: http.StatusServiceUnavailable, kind: xerr.KindUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(strings.Repeat("x", responseBodyMaxBytes+1)))
			}))
			defer server.Close()

			backend, err := NewHTTPBackend(HTTPConfig{Name: "test", Endpoint: server.URL})
			ktest.RequireNoError(t, err)
			_, err = backend.Decide(t.Context(), map[string]any{}, map[string]any{})
			ktest.RequireErrorKind(t, err, tc.kind)
			if len(err.Error()) > responseErrorBodyMaxBytes+len(server.URL)+128 {
				t.Fatalf("error message length = %d, want at most %d", len(err.Error()), responseErrorBodyMaxBytes+len(server.URL)+128)
			}
		})
	}
}
