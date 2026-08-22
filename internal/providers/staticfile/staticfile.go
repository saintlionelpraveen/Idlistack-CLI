// Package staticfile implements the static file provider.
// Detects: Static sites served via nginx (index.html, Staticfile, public/).
package staticfile

import (
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type StaticfileProvider struct{}

func (p *StaticfileProvider) Name() string { return "static" }

func (p *StaticfileProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	// Only detect as static if there's an index.html at root or in public/
	// AND no other provider files exist (to avoid false positives)
	if ctx.App.HasFile("Staticfile") {
		return true, nil
	}
	hasIndex := ctx.App.HasFile("index.html") || ctx.App.HasFile("public/index.html")
	if !hasIndex {
		return false, nil
	}
	// Exclude projects that have language-specific files
	hasLangFiles := ctx.App.HasFile("package.json") || ctx.App.HasFile("go.mod") ||
		ctx.App.HasFile("requirements.txt") || ctx.App.HasFile("Gemfile") ||
		ctx.App.HasFile("Cargo.toml") || ctx.App.HasFile("pom.xml") ||
		ctx.App.HasFile("composer.json") || ctx.App.HasFile("mix.exs")
	return !hasLangFiles, nil
}

func (p *StaticfileProvider) Initialize(ctx *provider.DetectContext) error { return nil }

func (p *StaticfileProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "static"
	plan.DetectedFramework = "static"
	plan.Runtime = "nginx"
	plan.Port = 80

	// Determine which directory to serve
	if ctx.App.HasFile("public/index.html") {
		plan.StaticDir = "public"
	} else {
		plan.StaticDir = "."
	}

	plan.StartCmd = "nginx -g 'daemon off;'"

	return plan, nil
}
