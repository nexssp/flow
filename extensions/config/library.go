// Package config provides the @config and @config.load directives that
// populate meta["config"], which OnPreprocess forwards to the compiler
// as a compile-time key-value map.
//
// Typical use:
//
//	@config:strict=true
//	@config {
//	  retries: 3       :cli="r,retries"
//	  timeout: "10s"   :cli="t"
//	}
//	@config.load:path="nexss.yml"
package config

import (
	"sync/atomic"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "config"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:           ID,
		Libraries:    []action.Library{{Name: ID}},
		Directives:   []core.Directive{ConfigDirective, ConfigLoadDirective},
		OnPreprocess: forwardConfigToCompiler,
		SelfTest:     selftest,
	}
}

// yamlLoader holds the optional YAML decoder registered by
// extensions/config_yaml. Nil when the YAML bundle is not linked in
// or was not @require'd by the flow.
var yamlLoader atomic.Pointer[func([]byte) (map[string]any, error)]

// RegisterYAMLLoader installs a YAML decoder. Called from the
// config_yaml bundle's init. Idempotent; later registrations win.
func RegisterYAMLLoader(fn func([]byte) (map[string]any, error)) {
	yamlLoader.Store(&fn)
}

// forwardConfigToCompiler hands the accumulated meta["config"] to the
// compile pipeline via WithConfigMap. The compiler resolves @config.x
// references through it.
func forwardConfigToCompiler(meta map[string]any) core.PreprocessContributions {
	cfg, _ := meta["config"].(map[string]string)
	if len(cfg) == 0 {
		return core.PreprocessContributions{}
	}
	return core.PreprocessContributions{
		CompileOpts: []core.CompileOption{core.WithConfigMap(cfg)},
	}
}

// metaConfig returns the mutable config map stored in out, creating it
// on first access. Directives append to it in source order.
func metaConfig(out map[string]any) map[string]string {
	cfg, _ := out["config"].(map[string]string)
	if cfg == nil {
		cfg = make(map[string]string)
		out["config"] = cfg
	}
	return cfg
}
