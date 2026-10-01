package decide

import (
	"context"
	"time"
)

// Backend is the contract every decision source implements.
type Backend interface {
	Name() string
	Decide(ctx context.Context, state, questions map[string]any) (Result, error)
	Close() error
}

// Result is the backend's response.
type Result struct {
	Answers map[string]Answer `json:"answers"`
	Routing map[string]any    `json:"routing,omitempty"`
}

// Answer is one question's resolved value.
type Answer struct {
	Value      any     `json:"value"`
	Confidence float64 `json:"confidence"`
	Label      string  `json:"label,omitempty"`
	Score      float64 `json:"score,omitempty"`
}

// Question describes one decision to make.
type Question struct {
	Type     string   `json:"type"`
	Labels   []string `json:"labels,omitempty"`
	Rubric   []string `json:"rubric,omitempty"`
	Question string   `json:"question,omitempty"`
}

// Request is the decide action's input.
type Request struct {
	Backend   string              `json:"backend" cli:"backend,b"`
	State     map[string]any      `json:"state,omitempty"`
	Questions map[string]Question `json:"questions"`
	TimeoutMS int64               `json:"timeout_ms,omitempty"`
}

// Response is the decide action's output. It mirrors Result so callers
// never need to import the backend contract.
type Response = Result

// HTTPConfig configures an HTTP-backed decision source.
type HTTPConfig struct {
	Name     string            `yaml:"name"`
	Endpoint string            `yaml:"endpoint"`
	Timeout  time.Duration     `yaml:"timeout"`
	Headers  map[string]string `yaml:"headers"`
}
