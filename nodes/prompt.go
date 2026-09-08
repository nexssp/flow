package nodes

import (
	"bytes"
	"context"
	"text/template"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type PromptConfig struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	SystemPrompt  string         `json:"system_prompt"`
	UserTemplate  string         `json:"user_template"`
	DefaultParams map[string]any `json:"default_params,omitempty"`
	Timeout       time.Duration  `json:"timeout,omitempty"`
}

type PromptReq struct {
	Input  any            `json:"input,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	Prompt string         `json:"prompt,omitempty"`
}

type PromptRes struct {
	SystemPrompt string         `json:"system_prompt"`
	RenderedUser string         `json:"rendered_user"`
	Params       map[string]any `json:"params"`
}

// NewPromptNode creates a reusable, pre-configured prompt template action.
func NewPromptNode(cfg PromptConfig) action.AnyAction {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}

	parsedTmpl := template.Must(template.New(cfg.Name).Parse(cfg.UserTemplate))

	return action.New(cfg.Name, func(ctx context.Context, req any) (PromptRes, error) {
		pReq := PromptReq{}

		switch v := req.(type) {
		case nil:
		case PromptReq:
			pReq = v
		case *PromptReq:
			if v != nil {
				pReq = *v
			}
		case string:
			pReq.Input = v
		case map[string]any:
			if input, ok := v["input"]; ok {
				pReq.Input = input
			}
			if rawParams, ok := v["params"]; ok {
				if params, ok := rawParams.(map[string]any); ok {
					pReq.Params = params
				}
			}
			if prompt, ok := v["prompt"]; ok {
				if s, ok := prompt.(string); ok {
					pReq.Prompt = s
				}
			}
		default:
			pReq.Input = req
		}

		params := make(map[string]any, len(cfg.DefaultParams)+len(pReq.Params))
		for k, v := range cfg.DefaultParams {
			params[k] = v
		}
		for k, v := range pReq.Params {
			params[k] = v
		}

		data := map[string]any{
			"input":  pReq.Input,
			"params": params,
		}

		var buf bytes.Buffer
		if err := parsedTmpl.Execute(&buf, data); err != nil {
			return PromptRes{}, xerr.Internal("prompt_node: template render failed", err)
		}

		sysPrompt := cfg.SystemPrompt
		if pReq.Prompt != "" {
			sysPrompt = pReq.Prompt
		}

		return PromptRes{
			SystemPrompt: sysPrompt,
			RenderedUser: buf.String(),
			Params:       params,
		}, nil
	}).
		Description(cfg.Description).
		Timeout(cfg.Timeout).
		Build()
}
