// Package deno implements the Deno provider.
// Detects: Deno applications via deno.json / deno.jsonc files.
package deno

import (
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type DenoProvider struct{}

func (p *DenoProvider) Name() string { return "deno" }

func (p *DenoProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("deno.json") || ctx.App.HasFile("deno.jsonc") ||
		ctx.App.HasFile("deno.lock"), nil
}

func (p *DenoProvider) Initialize(ctx *provider.DetectContext) error { return nil }

func (p *DenoProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "deno"
	plan.DetectedFramework = "deno"
	plan.Runtime = "latest"
	plan.Port = 8000

	// Check for Fresh framework
	if ctx.App.HasFile("fresh.gen.ts") || ctx.App.HasFileWithContent("deno.json", "fresh") {
		plan.DetectedFramework = "fresh"
		plan.StartCmd = "deno task start"
		plan.Port = 8000
	} else {
		// Generic Deno
		if ctx.App.HasFile("main.ts") {
			plan.StartCmd = "deno run --allow-net --allow-read --allow-env main.ts"
		} else if ctx.App.HasFile("mod.ts") {
			plan.StartCmd = "deno run --allow-net --allow-read --allow-env mod.ts"
		} else {
			plan.StartCmd = "deno task start"
		}
	}

	plan.Env = map[string]string{
		"DENO_DIR": "/app/.deno",
	}

	return plan, nil
}
