// path: nexssp/bootstrap/console/console.go
//
// Package console exposes a small, generic, self-contained web UI for any
// Nexss binary. It lists the registered actions and lets you invoke one
// with a JSON payload. It is intentionally minimal — a debugging surface,
// not an application console.
//
// Applications that want richer UIs (chat, sessions, docker timeline)
// should build them as separate actions and mount them alongside. The
// console package does not know about sessions, models, or business
// concepts.
package console

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/transport/thttp"
)

//go:embed console.html
var consoleHTML []byte

// Registry is the minimal source of actions the console lists. It is
// satisfied by anything that can enumerate the currently registered
// actions, including bootstrap.Assembly.
type Registry interface {
	Actions() []action.AnyAction
}

// RegistryFunc adapts a function to Registry.
type RegistryFunc func() []action.AnyAction

func (f RegistryFunc) Actions() []action.AnyAction { return f() }

// Config controls the mounted surface.
type Config struct {
	// Title is shown in the console header. Defaults to "Nexss Console".
	Title string

	// BasePath is the URL prefix. Defaults to "/console". Trailing slash
	// is stripped. Must start with "/".
	BasePath string

	// AllowExecute enables the "execute" panel. When false, the console
	// is read-only: it lists actions but cannot invoke them. Defaults to
	// false — read-only is the safer choice.
	AllowExecute bool
}

func (c Config) normalize() Config {
	if c.Title == "" {
		c.Title = "Nexss Console"
	}

	if c.BasePath == "" {
		c.BasePath = "/console"
	}

	c.BasePath = strings.TrimRight(c.BasePath, "/")
	if !strings.HasPrefix(c.BasePath, "/") {
		c.BasePath = "/" + c.BasePath
	}

	return c
}

// Actions returns the console surface for the given registry.
//
// Two actions are produced:
//
//	console.index    GET  <BasePath>/          — serves the HTML
//	console.actions  GET  <BasePath>/actions   — lists the registry as JSON
//
// When AllowExecute is true, a third action is added:
//
//	console.execute  POST <BasePath>/execute   — invokes a named action
//
// All three are Public-scoped so they can be reached through the normal
// HTTP transport. This is intentional: the console is a debugging
// surface, and the operator is expected to gate it at the reverse proxy
// or bind it to a private port, not hide it behind an auth scheme the
// framework does not own.
func Actions(registry Registry, cfg Config) []action.AnyAction {
	cfg = cfg.normalize()

	index := action.New[struct{}, struct{}]("console.index", nil).
		Description("Serves the Nexss debugging console").
		Tag("console").
		Public().
		Route(thttp.RawHandler(http.MethodGet, cfg.BasePath+"/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(consoleHTML)
		})).
		Build()

	list := action.New("console.actions", func(_ context.Context, _ struct{}) (listRes, error) {
		actions := registry.Actions()

		items := make([]ActionItem, 0, len(actions))
		for _, a := range actions {
			if a == nil || a.Describe() == nil {
				continue
			}

			m := a.Describe()
			items = append(items, ActionItem{
				Name:        m.Name,
				Description: m.Description,
				Tags:        m.Tags,
				Scope:       string(m.Scope),
			})
		}

		return listRes{Title: cfg.Title, Actions: items, AllowExecute: cfg.AllowExecute}, nil
	}).
		Description("Lists every action registered in this binary").
		Tag("console").
		Public().
		Route(thttp.GET(cfg.BasePath + "/actions")).
		Build()

	out := []action.AnyAction{index, list}
	if cfg.AllowExecute {
		out = append(out, buildExecute(cfg, registry))
	}

	return out
}

// ActionItem is the projection of action.Meta shown in the console.
// Deliberately narrow: no schema, no hooks, no internal fields.
type ActionItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Scope       string   `json:"scope,omitempty"`
}

type listRes struct {
	Title        string       `json:"title"`
	Actions      []ActionItem `json:"actions"`
	AllowExecute bool         `json:"allow_execute"`
}

type executeReq struct {
	Name    string          `json:"name"    validate:"required"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type executeRes struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// buildExecute returns the optional execute endpoint. It reuses the
// action registry's own ExecuteDecoded path, so the JSON input is
// validated against the action's own request schema.
func buildExecute(cfg Config, registry Registry) action.AnyAction {
	return action.New("console.execute", func(ctx context.Context, req executeReq) (executeRes, error) {
		if req.Name == "" {
			return executeRes{}, xerr.BadRequest("action name is required")
		}

		var target action.AnyAction

		for _, a := range registry.Actions() {
			if a == nil || a.Describe() == nil {
				continue
			}

			if a.Describe().Name == req.Name {
				target = a

				break
			}
		}

		if target == nil {
			return executeRes{}, xerr.NotFound("action not found: " + req.Name)
		}

		ex, ok := target.(action.Executable)
		if !ok {
			return executeRes{}, xerr.Internal("action is not executable")
		}

		raw := req.Payload
		if len(raw) == 0 || string(raw) == "null" {
			raw = json.RawMessage("{}")
		}

		result, err := ex.ExecuteDecoded(ctx, func(v any) error {
			return json.Unmarshal(raw, v)
		})
		if err != nil {
			return executeRes{Error: err.Error()}, nil
		}

		return executeRes{Result: result}, nil
	}).
		Description("Invokes a registered action by name").
		Tag("console").
		Public().
		Route(thttp.POST(cfg.BasePath + "/execute")).
		Build()
}
