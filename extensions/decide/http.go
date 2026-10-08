package decide

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nexssp/kernel/xerr"
)

// httpBackend is the default HTTP-backed decision source.
type httpBackend struct {
	configuration HTTPConfig
	client        *http.Client
}

// NewHTTPBackend builds an HTTP decision source. A zero Timeout
// defaults to 30 seconds.
func NewHTTPBackend(configuration HTTPConfig) (Backend, error) {
	if configuration.Name == "" {
		return nil, xerr.Validation("decide/http: backend name is required")
	}
	if configuration.Endpoint == "" {
		return nil, xerr.Validation("decide/http: endpoint is required")
	}
	timeout := configuration.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &httpBackend{
		configuration: configuration,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        32,
				MaxIdleConnsPerHost: 32,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}, nil
}

func (b *httpBackend) Name() string { return b.configuration.Name }
func (b *httpBackend) Close() error { return nil }

func (b *httpBackend) Decide(ctx context.Context, state, questions map[string]any) (Result, error) {
	requestBody, err := json.Marshal(map[string]any{
		"state":     state,
		"questions": questions,
	})
	if err != nil {
		return Result{}, xerr.Internal("decide/http: marshal request", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.configuration.Endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return Result{}, xerr.Internal("decide/http: build request", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	for key, value := range b.configuration.Headers {
		httpRequest.Header.Set(key, value)
	}

	response, err := b.client.Do(httpRequest)
	if err != nil {
		return Result{}, xerr.Unavailable("decide/http: dispatch "+b.configuration.Endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return Result{}, xerr.Unavailable("decide/http: read response", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, errorFromStatus(response.StatusCode, b.configuration.Endpoint, body)
	}

	var result Result
	if err := json.Unmarshal(body, &result); err != nil {
		return Result{}, xerr.Internal("decide/http: unmarshal response", err)
	}
	return result, nil
}

// errorFromStatus maps an HTTP status to the matching xerr kind so
// retry and circuit-breaker policies can react correctly. 5xx is
// transient (Unavailable); 4xx is permanent (BadRequest family);
// 429 is its own transient kind.
func errorFromStatus(status int, endpoint string, body []byte) error {
	msg := fmt.Sprintf("decide/http: %s returned %d: %s", endpoint, status, body)
	switch status {
	case http.StatusBadRequest:
		return xerr.BadRequest(msg)
	case http.StatusUnauthorized:
		return xerr.Unauthorized(msg)
	case http.StatusForbidden:
		return xerr.Forbidden(msg)
	case http.StatusNotFound:
		return xerr.NotFound(msg)
	case http.StatusConflict:
		return xerr.Conflict(msg)
	case http.StatusTooManyRequests:
		return xerr.TooManyRequests(msg)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return xerr.Timeout(msg)
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return xerr.Unavailable(msg)
	default:
		if status >= 500 {
			return xerr.Internal(msg)
		}
		return xerr.BadRequest(msg)
	}
}
