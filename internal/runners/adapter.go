package runners

import (
	"context"

	"github.com/afittestide/asimi/shogunate"
)

// ShogunateAdapter adapts internal/runners.Runner to shogunate.Runner interface.
// This allows the runners package types to be used with the shogunate package.
type ShogunateAdapter struct {
	runner Runner
}

// NewShogunateAdapter creates a new adapter wrapping a Runner.
func NewShogunateAdapter(r Runner) *ShogunateAdapter {
	if r == nil {
		return nil
	}
	return &ShogunateAdapter{runner: r}
}

// Run executes a command and returns the result.
func (a *ShogunateAdapter) Run(ctx context.Context, input shogunate.RunnerInput) (shogunate.RunnerOutput, error) {
	out, err := a.runner.Run(ctx, Input{
		Command:        input.Command,
		Description:    input.Description,
		BypassApproval: input.BypassApproval,
	})
	return shogunate.RunnerOutput{
		Output:   out.Output,
		ExitCode: out.ExitCode,
	}, err
}

// Restart restarts the underlying runner.
func (a *ShogunateAdapter) Restart(ctx context.Context) error {
	return a.runner.Restart(ctx)
}

// Close closes the underlying runner.
func (a *ShogunateAdapter) Close(ctx context.Context) error {
	return a.runner.Close(ctx)
}

// RunnerType returns the type of the underlying runner.
func (a *ShogunateAdapter) RunnerType() string {
	return a.runner.RunnerType()
}
