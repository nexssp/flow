package config_runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfs"

	configext "github.com/nexssp/flow/extensions/config"
)

// LoadRequest names one file relative to the host-configured root.
type LoadRequest struct {
	Path string `json:"path" validate:"required"`
}

func loadAction(root *xfs.Root, rootErr error) action.AnyAction {
	return action.New("config_runtime.load", func(_ context.Context, req LoadRequest) (map[string]any, error) {
		if rootErr != nil || root == nil {
			return nil, xerr.Unavailable("config_runtime.load: host-configured root is unavailable", rootErr)
		}
		if strings.TrimSpace(req.Path) == "" {
			return nil, xerr.BadRequest("config_runtime.load: path is required")
		}

		data, err := root.ReadFile(req.Path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, xerr.NotFound("config_runtime.load: config file not found", err)
			}
			// Preserve xfs's typed path rejection (including traversal attempts).
			if xerr.KindFrom(err) == xerr.KindForbidden || xerr.KindFrom(err) == xerr.KindBadRequest {
				return nil, err
			}
			return nil, xerr.Internal("config_runtime.load: could not read config file", err)
		}

		ext := strings.ToLower(filepath.Ext(req.Path))
		values, err := configext.DecodeFile(data, ext)
		if err != nil {
			return nil, xerr.BadRequest(fmt.Sprintf("config_runtime.load: could not decode %s config", ext), err)
		}
		if values == nil {
			return nil, xerr.BadRequest("config_runtime.load: config document must be an object")
		}
		return values, nil
	}).Description("Load a structured config object from the host-configured root").Build()
}
