package decide

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
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
		return nil, errors.New("decide/http: backend name is required")
	}
	if configuration.Endpoint == "" {
		return nil, errors.New("decide/http: endpoint is required")
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
		return Result{}, fmt.Errorf("decide/http: marshal request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.configuration.Endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return Result{}, fmt.Errorf("decide/http: build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	for key, value := range b.configuration.Headers {
		httpRequest.Header.Set(key, value)
	}

	response, err := b.client.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("decide/http: dispatch: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return Result{}, fmt.Errorf("decide/http: read response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, fmt.Errorf("decide/http: status %d: %s", response.StatusCode, body)
	}

	var result Result
	if err := json.Unmarshal(body, &result); err != nil {
		return Result{}, fmt.Errorf("decide/http: unmarshal: %w", err)
	}
	return result, nil
}
