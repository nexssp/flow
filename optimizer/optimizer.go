package optimizer

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

type Evaluator func(ctx context.Context, candidate action.AnyAction) (score float64, err error)

type Options struct {
	Generations int
	Population  int
}

type Candidate struct {
	DSL   string
	Score float64
}

// Evolve searches for the optimal pipeline architecture using genetic mutation.
func Evolve(
	ctx context.Context,
	baselineDSL string,
	reg flow.Registry,
	eval Evaluator,
	opts Options,
) (Candidate, error) {
	if opts.Generations <= 0 {
		opts.Generations = 3
	}
	if opts.Population <= 0 {
		opts.Population = 4
	}

	baselineBld, err := flow.CompilePipeline(baselineDSL, reg)
	if err != nil {
		return Candidate{}, fmt.Errorf("optimizer: invalid baseline DSL: %w", err)
	}

	bestScore, err := eval(ctx, baselineBld.Build())
	if err != nil {
		return Candidate{}, fmt.Errorf("optimizer: baseline evaluation failed: %w", err)
	}

	best := Candidate{DSL: baselineDSL, Score: bestScore}

	actions := reg.Actions()
	actionNames := make([]string, 0, len(actions))
	for _, act := range actions {
		actionNames = append(actionNames, act.Describe().Name)
	}

	for gen := 1; gen <= opts.Generations; gen++ {
		candidates := make([]Candidate, 0, opts.Population)

		for i := 0; i < opts.Population; i++ {
			mutatedDSL := mutate(best.DSL, actionNames)

			compiled, compileErr := flow.CompilePipeline(mutatedDSL, reg)
			if compileErr != nil {
				continue
			}

			score, evalErr := eval(ctx, compiled.Build())
			if evalErr != nil {
				continue
			}

			candidates = append(candidates, Candidate{DSL: mutatedDSL, Score: score})
		}

		for _, c := range candidates {
			if c.Score > best.Score {
				best = c
			}
		}
	}

	return best, nil
}

func mutate(dsl string, availableActions []string) string {
	mutations := [...]func(string, []string) string{
		mutateAddRetry,
		mutateParallelize,
		mutateAddFallback,
	}

	//nolint:gosec // Fast non-cryptographic PRNG is intentional for genetic exploration
	fn := mutations[rand.IntN(len(mutations))]
	return fn(dsl, availableActions)
}

func mutateAddRetry(dsl string, _ []string) string {
	nodes := strings.Split(dsl, "->")
	if len(nodes) == 0 {
		return dsl
	}
	//nolint:gosec // Pseudo-random index selection for genetic mutations
	targetIdx := rand.IntN(len(nodes))
	target := strings.TrimSpace(nodes[targetIdx])

	if !strings.Contains(target, "retry=") && !strings.Contains(target, "{") && !strings.Contains(target, "(") {
		nodes[targetIdx] = target + ":retry=2"
	}
	return strings.Join(nodes, " -> ")
}

func mutateParallelize(dsl string, _ []string) string {
	parts := strings.Split(dsl, "->")
	if len(parts) < 3 {
		return dsl
	}

	//nolint:gosec // Pseudo-random node pairing for parallelization
	idx := rand.IntN(len(parts) - 1)
	left := strings.TrimSpace(parts[idx])
	right := strings.TrimSpace(parts[idx+1])

	if !strings.Contains(left, "{") && !strings.Contains(right, "{") &&
		!strings.Contains(left, "(") && !strings.Contains(right, "(") {
		parallelGroup := fmt.Sprintf("( %s & %s )", left, right)

		newParts := make([]string, 0, len(parts)-1)
		for i := 0; i < idx; i++ {
			newParts = append(newParts, parts[i])
		}
		newParts = append(newParts, parallelGroup)
		for i := idx + 2; i < len(parts); i++ {
			newParts = append(newParts, parts[i])
		}
		return strings.Join(newParts, " -> ")
	}

	return dsl
}

func mutateAddFallback(dsl string, available []string) string {
	if len(available) == 0 {
		return dsl
	}
	//nolint:gosec // Pseudo-random fallback selection
	fallback := available[rand.IntN(len(available))]

	parts := strings.Split(dsl, "->")
	//nolint:gosec // Pseudo-random target node selection
	targetIdx := rand.IntN(len(parts))
	target := strings.TrimSpace(parts[targetIdx])

	if !strings.Contains(target, "||") && !strings.Contains(target, "{") && !strings.Contains(target, "(") {
		parts[targetIdx] = fmt.Sprintf("( %s || %s )", target, fallback)
	}
	return strings.Join(parts, " -> ")
}
