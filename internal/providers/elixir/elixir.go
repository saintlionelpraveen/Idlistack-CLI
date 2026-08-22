// Package elixir implements the Elixir provider.
// Detects: Phoenix and generic Elixir/Mix projects.
package elixir

import (
	"regexp"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type ElixirProvider struct {
	framework string
	version   string
}

func (p *ElixirProvider) Name() string { return "elixir" }

func (p *ElixirProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("mix.exs"), nil
}

func (p *ElixirProvider) Initialize(ctx *provider.DetectContext) error {
	p.framework = "elixir"
	if ctx.App.HasDir("lib") && ctx.App.HasFileWithContent("mix.exs", "phoenix") {
		p.framework = "phoenix"
	}
	p.version = "latest"
	if ctx.App.HasFile(".elixir-version") {
		if content, err := ctx.App.ReadFileString(".elixir-version"); err == nil {
			re := regexp.MustCompile(`(\d+\.\d+)`)
			if m := re.FindString(content); m != "" {
				p.version = m
			}
		}
	}
	return nil
}

func (p *ElixirProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "elixir"
	plan.DetectedFramework = p.framework
	plan.Runtime = p.version

	plan.InstallCmd = "mix deps.get --only prod"
	plan.BuildCmd = "MIX_ENV=prod mix compile && MIX_ENV=prod mix assets.deploy 2>/dev/null; true"

	if p.framework == "phoenix" {
		plan.Port = 4000
		plan.StartCmd = "MIX_ENV=prod mix phx.server"
		plan.Env = map[string]string{
			"MIX_ENV":    "prod",
			"PHX_HOST":   "0.0.0.0",
			"PORT":       "4000",
			"SECRET_KEY_BASE": "generate-me",
		}
	} else {
		plan.Port = 4000
		plan.StartCmd = "MIX_ENV=prod mix run --no-halt"
		plan.Env = map[string]string{"MIX_ENV": "prod"}
	}

	return plan, nil
}
