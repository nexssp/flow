package config_yaml

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	configext "github.com/nexssp/flow/extensions/config"
)

// LoadRequest names a YAML file to load at runtime.
type LoadRequest struct {
	Path string `json:"path" validate:"required"`
}

// LoadAction loads a structured YAML object from the local filesystem.
var LoadAction = action.New("config_yaml.load", func(ctx context.Context, req LoadRequest) (map[string]any, error) {
	if strings.TrimSpace(req.Path) == "" {
		return nil, xerr.BadRequest("config_yaml.load: path is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(req.Path))
	if ext != ".yaml" && ext != ".yml" {
		return nil, xerr.BadRequest("config_yaml.load: file extension must be .yaml or .yml")
	}

	data, err := os.ReadFile(req.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, xerr.NotFound("config_yaml.load: YAML file not found", err)
		}
		return nil, xerr.Internal("config_yaml.load: could not read YAML file", err)
	}
	values, err := configext.DecodeFile(data, ext)
	if err != nil {
		return nil, xerr.BadRequest("config_yaml.load: could not decode YAML file", err)
	}
	if values == nil {
		return nil, xerr.BadRequest("config_yaml.load: YAML document must be an object")
	}
	return values, nil
}).Description("Load a structured YAML object from a file at runtime").Tag("config", "yaml", "load").Build()
