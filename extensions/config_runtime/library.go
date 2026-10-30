// Package config_runtime provides a host-enabled runtime config file loader.
// Unlike @config.load, it returns structured values to the executing pipeline.
// The host must supply the trusted file root; flow source cannot set it.
//
// Typical host setup:
//
// bundle, err := config_runtime.BundleWithRoot("/etc/myapp/config")
//
// Typical flow use:
//
// {} -> config_runtime.load @{ path: "settings.json" } -> AppConfig.validate
package config_runtime

import (
	"context"
	"embed"
	"errors"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xfs"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "config_runtime"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

// Bundle is the source-level @require factory. It intentionally does not accept
// filesystem roots from flow source; only a host may provide the trusted root
// through BundleWithRoot.
func Bundle(_ map[string]string) compiler.Bundle {
	return newBundle(nil, errors.New("host-configured root is required; use BundleWithRoot"))
}

// BundleWithRoot enables the runtime loader for a host using an explicit,
// trusted directory. Paths supplied to config_runtime.load are always relative
// to this root. The host owns the returned bundle and should run its Shutdowns
// after the runner invocation completes.
func BundleWithRoot(rootPath string) (compiler.Bundle, error) {
	if strings.TrimSpace(rootPath) == "" {
		return compiler.Bundle{}, errors.New("config_runtime: host root is required")
	}
	root, err := xfs.OpenRoot(rootPath)
	if err != nil {
		return compiler.Bundle{}, err
	}
	return newBundle(root, nil), nil
}

func newBundle(root *xfs.Root, rootErr error) compiler.Bundle {
	bundle := compiler.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name:    ID,
			Actions: []action.AnyAction{loadAction(root, rootErr)},
		}},
		AcceptedOptions: []string{},
		Fixtures:        fixturesFS,
	}
	if root != nil {
		bundle.Shutdowns = []compiler.ShutdownFunc{func(_ context.Context) error {
			return root.Close()
		}}
	}
	return bundle
}
