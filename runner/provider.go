package runner

import (
	"os"
	"time"

	"github.com/nexssp/ai/llm"
)

// ResolveProvider picks the best available LLM provider from the environment,
// falling back to a local Ollama or, ultimately, a mock.
//
// Detection order (highest priority first):
//
//	DEEPSEEK_API_KEY  -> deepseek-chat
//	OPENAI_API_KEY    -> gpt-4o-mini
//	ANTHROPIC_API_KEY -> claude-3-5-sonnet-20241022
//	(no key)          -> http://localhost:11434 qwen2.5-coder:7b
//	(unreachable)     -> deterministic mock
func ResolveProvider() llm.Provider {
	if k := os.Getenv("DEEPSEEK_API_KEY"); k != "" {
		if p, err := llm.NewProvider("deepseek", llm.Config{
			APIKey:       k,
			DefaultModel: envOr("DEEPSEEK_MODEL", "deepseek-chat"),
			Timeout:      30 * time.Second,
		}); err == nil {
			return p
		}
	}

	if k := os.Getenv("OPENAI_API_KEY"); k != "" {
		if p, err := llm.NewProvider("openai", llm.Config{
			APIKey:       k,
			DefaultModel: envOr("OPENAI_MODEL", "gpt-4o-mini"),
			Timeout:      30 * time.Second,
		}); err == nil {
			return p
		}
	}

	if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
		if p, err := llm.NewProvider("anthropic", llm.Config{
			APIKey:       k,
			DefaultModel: envOr("ANTHROPIC_MODEL", "claude-3-5-sonnet-20241022"),
			Timeout:      30 * time.Second,
		}); err == nil {
			return p
		}
	}

	if p, err := llm.NewProvider("ollama", llm.Config{
		BaseURL:      envOr("OLLAMA_URL", "http://localhost:11434"),
		DefaultModel: envOr("OLLAMA_MODEL", "qwen2.5-coder:7b"),
		Timeout:      30 * time.Second,
	}); err == nil {
		return p
	}

	p, _ := llm.NewProvider("openai", llm.Config{DefaultModel: "mock"})

	return p
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
