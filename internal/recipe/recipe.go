package recipe

import (
	"context"
	"fmt"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
	"github.com/agent462/herd/internal/selector"
)

// Step represents a single command in a recipe, optionally scoped to a selector.
type Step struct {
	Selector string // "" means @all
	Command  string
}

// StepResult holds the outcome of executing a single recipe step.
type StepResult struct {
	Step    Step
	Hosts   []string
	Results []*executor.HostResult
	Grouped *grouper.GroupedResults
}

// ParseStep parses a raw step string into a Step using selector.ParseInput.
func ParseStep(raw string) Step {
	sel, cmd := selector.ParseInput(raw)
	return Step{Selector: sel, Command: cmd}
}

// Runner executes recipe steps sequentially with selector propagation.
type Runner struct {
	exec     *executor.Executor
	allHosts []string
}

// New creates a Runner with the given executor and full host list.
func New(exec *executor.Executor, hosts []string) *Runner {
	return &Runner{
		exec:     exec,
		allHosts: hosts,
	}
}

// Run executes steps sequentially. After each step, the selector State is
// updated with the step's GroupedResults, so @differs/@ok/@failed in step N
// references step N-1's results.
func (r *Runner) Run(ctx context.Context, steps []Step) ([]StepResult, error) {
	state := &selector.State{
		AllHosts: r.allHosts,
	}

	results := make([]StepResult, 0, len(steps))

	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return results, fmt.Errorf("recipe cancelled: %w", err)
		}

		hosts, err := selector.Resolve(step.Selector, state)
		if err != nil {
			return results, fmt.Errorf("step %q: %w", step.Command, err)
		}

		hostResults := r.exec.Execute(ctx, hosts, step.Command)
		grouped := grouper.Group(hostResults)

		results = append(results, StepResult{
			Step:    step,
			Hosts:   hosts,
			Results: hostResults,
			Grouped: grouped,
		})

		// Propagate grouped results so the next step can use @ok, @differs, etc.
		state.Grouped = grouped
	}

	return results, nil
}
