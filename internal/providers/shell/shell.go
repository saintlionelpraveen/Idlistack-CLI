// Package shell implements the universal shell fallback provider.
// This is the last resort — detects any project with shell scripts.
package shell

import (
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type ShellProvider struct {
	entryScript string
}

func (p *ShellProvider) Name() string { return "shell" }

func (p *ShellProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	// Check for common entry scripts
	scripts := []string{"start.sh", "run.sh", "entrypoint.sh", "app.sh"}
	for _, s := range scripts {
		if ctx.App.HasFile(s) {
			return true, nil
		}
	}
	// Check for any .sh file
	files, err := ctx.App.FindFiles("*.sh")
	return len(files) > 0, err
}

func (p *ShellProvider) Initialize(ctx *provider.DetectContext) error {
	// Prefer known entry scripts
	scripts := []string{"start.sh", "run.sh", "entrypoint.sh", "app.sh"}
	for _, s := range scripts {
		if ctx.App.HasFile(s) {
			p.entryScript = s
			return nil
		}
	}
	// Fallback to first .sh file found
	if files, err := ctx.App.FindFiles("*.sh"); err == nil && len(files) > 0 {
		p.entryScript = files[0]
	}
	return nil
}

func (p *ShellProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "shell"
	plan.DetectedFramework = "shell"
	plan.Runtime = "ubuntu"
	plan.Port = 8080

	plan.StartCmd = "bash " + p.entryScript

	return plan, nil
}
