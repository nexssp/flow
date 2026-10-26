package compiler

import (
	"context"
	"sync"
	"testing"
)

func TestApplyPreprocessContributionsDoesNotMutateSharedOptions(t *testing.T) {
	backing := make([]CompileOption, 2)
	backing[0] = WithActionIntrospection()
	backing[1] = WithActionGraph()
	sharedOpts := backing[:1]
	contributions := PreprocessContributions{
		CompileOpts: []CompileOption{WithConfigMap(map[string]string{"contribution": "merged"})},
	}

	const workers = 24
	const iterations = 128
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			for range iterations {
				cfg, _, _ := applyPreprocessContributions(
					context.Background(), &compileConfig{}, sharedOpts, nil, contributions, CompileReq{},
				)
				if !cfg.actionIntrospection || cfg.config["contribution"] != "merged" {
					t.Errorf("merged config = %#v; want introspection and contribution options", cfg)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	if !applyCompileOptions(backing).actionGraph {
		t.Fatal("shared compile options were mutated: trailing option was overwritten")
	}
}
