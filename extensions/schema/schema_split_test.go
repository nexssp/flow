package schema

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestSplitFields(t *testing.T) {
	t.Parallel()

	s := Schema{
		Name: "Test",
		Fields: []Field{
			{JSONName: "default_payload"},
			{JSONName: "explicit_payload", Tags: map[string]string{"payload": "true"}},
			{JSONName: "config_only", Tags: map[string]string{"config": "true"}},
			{JSONName: "both", Tags: map[string]string{"config": "true", "payload": "true"}},
			{JSONName: "config_false", Tags: map[string]string{"config": "false"}},
			{JSONName: "config_other", Tags: map[string]string{"config": "yes"}},
		},
	}

	payload, config := SplitFields(s)

	ktest.RequireEqual(t, payload, []string{
		"default_payload",
		"explicit_payload",
		"both",
		"config_false",
		"config_other",
	})
	ktest.RequireEqual(t, config, []string{
		"config_only",
		"both",
	})
}
