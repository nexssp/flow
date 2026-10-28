package nodes_supervisor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"golang.org/x/sync/errgroup"

	"github.com/nexssp/flow/compiler/contracts"
)

// ServiceSpec describes one long-lived action in a supervisor. An omitted
// Enabled field means true; a false value excludes the service from startup.
type ServiceSpec struct {
	ID        string   `json:"id"`
	Action    string   `json:"action"`
	DependsOn []string `json:"depends_on,omitempty"`
	Enabled   *bool    `json:"enabled,omitempty"`
	Payload   any      `json:"payload,omitempty"`
}

// ServeReq is the static service graph accepted by supervisor.serve.
type ServeReq struct {
	Services []ServiceSpec `json:"services"`
}

type serviceNode struct {
	id        string
	action    string
	dependsOn []string
	enabled   bool
	payload   any
}

type servicePlan struct {
	id           string
	action       string
	dependencies []int
	enabled      bool
	payload      any
}

// Serve starts long-lived actions, waits for their declared dependencies to
// become ready, and cancels siblings when any service exits unexpectedly.
// Parent-context cancellation is a clean shutdown. Services must honor their
// context; Go cannot forcibly stop a non-cooperative action.
var Serve = action.New("supervisor.serve", func(ctx context.Context, req ServeReq) (any, error) {
	if ctx == nil {
		return nil, xerr.BadRequest("supervisor.serve: context is nil")
	}

	nodes := make([]serviceNode, len(req.Services))
	for i, service := range req.Services {
		enabled := service.Enabled == nil || *service.Enabled
		nodes[i] = serviceNode{
			id:        service.ID,
			action:    service.Action,
			dependsOn: service.DependsOn,
			enabled:   enabled,
			payload:   service.Payload,
		}
	}
	plan, err := buildServicePlan(nodes)
	if err != nil {
		return nil, xerr.BadRequest("supervisor.serve: " + err.Error())
	}

	resolver := contracts.ActionResolverFromContext(ctx)
	if resolver == nil {
		return nil, xerr.Internal("supervisor.serve: no action resolver in context")
	}
	actions := make([]action.AnyAction, len(plan))
	enabledCount := 0
	for i, service := range plan {
		act, found := resolver.Action(service.action)
		if !found || act == nil {
			return nil, xerr.NotFound("supervisor.serve: action " + service.action + " not found")
		}
		actions[i] = act
		if service.enabled {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return nil, xerr.BadRequest("supervisor.serve: no enabled services")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil //nolint:nilnil // parent cancellation before startup is a clean shutdown
	}

	ready := make([]*contracts.ReadySignal, len(plan))
	for i := range ready {
		ready[i] = contracts.NewReadySignal()
	}
	group, groupCtx := errgroup.WithContext(ctx)
	for i := range plan {
		if !plan[i].enabled {
			continue
		}
		current, act, signal := plan[i], actions[i], ready[i]
		group.Go(func() (serviceErr error) {
			defer func() {
				signal.MarkStopped()
				if recovered := recover(); recovered != nil {
					serviceErr = fmt.Errorf("service %q panicked: %v", current.id, recovered)
				}
			}()

			for _, dependency := range current.dependencies {
				select {
				case <-ready[dependency].Done():
				case <-groupCtx.Done():
					return nil
				}
			}
			if groupCtx.Err() != nil {
				return nil
			}

			serviceCtx := contracts.WithServiceReady(groupCtx, signal)
			_, invokeErr := action.InvokeAny(serviceCtx, act, current.payload)
			if ctx.Err() != nil {
				return nil
			}
			if invokeErr == nil {
				return fmt.Errorf("service %q exited unexpectedly without an error", current.id)
			}
			return fmt.Errorf("service %q exited unexpectedly: %w", current.id, invokeErr)
		})
	}

	return nil, group.Wait()
}).
	Description("Run long-lived services with readiness dependencies and sibling cancellation").
	Tag("supervisor", "serve").
	Build()

func buildServicePlan(nodes []serviceNode) ([]servicePlan, error) {
	if len(nodes) == 0 {
		return nil, errors.New("services must be a non-empty list")
	}

	plan := make([]servicePlan, len(nodes))
	indices := make(map[string]int, len(nodes))
	for i, node := range nodes {
		id := strings.TrimSpace(node.id)
		actionName := strings.TrimSpace(node.action)
		if id == "" {
			return nil, fmt.Errorf("services[%d] is missing a non-empty id", i)
		}
		if actionName == "" {
			return nil, fmt.Errorf("service %q is missing a non-empty action", id)
		}
		if _, exists := indices[id]; exists {
			return nil, fmt.Errorf("duplicate service id %q", id)
		}
		indices[id] = i
		plan[i] = servicePlan{
			id:      id,
			action:  actionName,
			enabled: node.enabled,
			payload: node.payload,
		}
	}

	dependents := make([][]int, len(plan))
	remainingDependencies := make([]int, len(plan))
	for i, node := range nodes {
		for _, rawDependency := range node.dependsOn {
			dependencyName := strings.TrimSpace(rawDependency)
			if dependencyName == "" {
				return nil, fmt.Errorf("service %q has an empty dependency id", plan[i].id)
			}
			dependency, exists := indices[dependencyName]
			if !exists {
				return nil, fmt.Errorf("service %q depends on unknown service %q", plan[i].id, dependencyName)
			}
			plan[i].dependencies = append(plan[i].dependencies, dependency)
			dependents[dependency] = append(dependents[dependency], i)
			remainingDependencies[i]++
			if plan[i].enabled && !plan[dependency].enabled {
				return nil, fmt.Errorf("enabled service %q depends on disabled service %q", plan[i].id, dependencyName)
			}
		}
	}

	readyQueue := make([]int, 0, len(plan))
	for i, count := range remainingDependencies {
		if count == 0 {
			readyQueue = append(readyQueue, i)
		}
	}
	visited := 0
	for head := 0; head < len(readyQueue); head++ {
		visited++
		for _, dependent := range dependents[readyQueue[head]] {
			remainingDependencies[dependent]--
			if remainingDependencies[dependent] == 0 {
				readyQueue = append(readyQueue, dependent)
			}
		}
	}
	if visited != len(plan) {
		cycle := make([]string, 0, len(plan)-visited)
		for i, count := range remainingDependencies {
			if count > 0 {
				cycle = append(cycle, plan[i].id)
			}
		}
		return nil, fmt.Errorf("dependency cycle involves services %q", strings.Join(cycle, ", "))
	}
	return plan, nil
}
