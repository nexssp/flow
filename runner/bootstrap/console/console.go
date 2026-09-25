package console

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

//go:embed console.html
var consoleHTML []byte

type Registry interface {
	Actions() []action.AnyAction
}

type RegistryFunc func() []action.AnyAction

func (f RegistryFunc) Actions() []action.AnyAction { return f() }

type Config struct {
	Title        string
	BasePath     string
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

// Neutralne definicje tras HTTP bez zależności od zewnętrznego pakietu thttp
type routeBinding struct {
	Method string
	Path   string
}

func (r routeBinding) HTTPRoute() (string, string) { return r.Method, r.Path }

type rawRouteBinding struct {
	Method  string
	Path    string
	Handler http.HandlerFunc
}

func (r rawRouteBinding) RawHTTPHandler() (string, string, http.HandlerFunc) {
	return r.Method, r.Path, r.Handler
}

func Actions(registry Registry, cfg Config) []action.AnyAction {
	cfg = cfg.normalize()

	index := action.New[struct{}, struct{}]("console.index", nil).
		Description("Serves the Nexss debugging console").
		Tag("console").
		Public().
		Route(rawRouteBinding{
			Method: http.MethodGet,
			Path:   cfg.BasePath + "/",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write(consoleHTML)
			},
		}).
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
		Route(routeBinding{Method: http.MethodGet, Path: cfg.BasePath + "/actions"}).
		Build()

	out := []action.AnyAction{index, list}
	if cfg.AllowExecute {
		out = append(out, buildExecute(cfg, registry))
	}

	return out
}

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
	Name    string          `json:"name" validate:"required"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type executeRes struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

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
		Route(routeBinding{Method: http.MethodPost, Path: cfg.BasePath + "/execute"}).
		Build()
}
