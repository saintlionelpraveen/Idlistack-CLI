// Package rust implements the Rust provider.
// Detects: Rust applications via Cargo.toml.
package rust

import (
	"regexp"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type RustProvider struct {
	framework string
	binName   string
}

func (p *RustProvider) Name() string {
	return "rust"
}

func (p *RustProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("Cargo.toml"), nil
}

func (p *RustProvider) Initialize(ctx *provider.DetectContext) error {
	p.binName = p.detectBinaryName(ctx)
	p.framework = p.detectFramework(ctx)
	return nil
}

func (p *RustProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "rust"
	plan.DetectedFramework = p.framework
	plan.Runtime = "latest"
	plan.Port = 8080

	plan.InstallCmd = ""
	plan.BuildCmd = "cargo build --release"
	plan.StartCmd = "./target/release/" + p.binName

	plan.Env = map[string]string{
		"ROCKET_ADDRESS": "0.0.0.0",
	}

	return plan, nil
}

func (p *RustProvider) detectBinaryName(ctx *provider.DetectContext) string {
	if ctx.App.HasFile("Cargo.toml") {
		if content, err := ctx.App.ReadFileString("Cargo.toml"); err == nil {
			re := regexp.MustCompile(`(?m)^\s*name\s*=\s*"([^"]+)"`)
			if matches := re.FindStringSubmatch(content); len(matches) > 1 {
				return matches[1]
			}
		}
	}
	return "app"
}

func (p *RustProvider) detectFramework(ctx *provider.DetectContext) string {
	if ctx.App.HasFile("Cargo.toml") {
		if ctx.App.HasFileWithContent("Cargo.toml", "actix-web") {
			return "actix"
		}
		if ctx.App.HasFileWithContent("Cargo.toml", "rocket") {
			return "rocket"
		}
		if ctx.App.HasFileWithContent("Cargo.toml", "axum") {
			return "axum"
		}
		if ctx.App.HasFileWithContent("Cargo.toml", "warp") {
			return "warp"
		}
	}
	return "rust"
}
