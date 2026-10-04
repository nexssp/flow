package main

// config_yaml registers its bundle so `@require config_yaml` resolves,
// but it is not in native.Bundles(): linking gopkg.in/yaml.v3 into
// every nflow binary is not acceptable for a feature most users never
// enable. Its fixtures begin with `@require config_yaml`.
//
// Every other bundle the CLI ships is imported by native, not here.
// Adding a blank import in this file creates an environment that
// `nflow run` cannot see. Do not do it.
import (
	_ "github.com/nexssp/flow/extensions/config_yaml"
)
