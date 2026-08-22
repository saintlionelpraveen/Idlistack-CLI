// Package ruby implements the Ruby provider.
// Detects: Rails, Sinatra, and generic Ruby apps.
package ruby

import (
	"regexp"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type RubyProvider struct {
	framework string
	version   string
}

func (p *RubyProvider) Name() string {
	return "ruby"
}

func (p *RubyProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("Gemfile"), nil
}

func (p *RubyProvider) Initialize(ctx *provider.DetectContext) error {
	p.framework = p.detectFramework(ctx)
	p.version = p.detectVersion(ctx)
	return nil
}

func (p *RubyProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "ruby"
	plan.DetectedFramework = p.framework
	plan.Runtime = p.version
	plan.InstallCmd = "bundle install"

	switch p.framework {
	case "rails":
		plan.BuildCmd = "bundle exec rake assets:precompile"
		plan.StartCmd = "bundle exec rails server -b 0.0.0.0 -p 3000"
		plan.Port = 3000
		plan.Env = map[string]string{
			"RAILS_ENV":                "production",
			"RAILS_SERVE_STATIC_FILES": "true",
			"RAILS_LOG_TO_STDOUT":      "true",
		}
	case "sinatra":
		plan.StartCmd = "bundle exec ruby app.rb"
		plan.Port = 4567
	default:
		plan.StartCmd = "bundle exec ruby app.rb"
		plan.Port = 9292
	}

	return plan, nil
}

func (p *RubyProvider) detectFramework(ctx *provider.DetectContext) string {
	if ctx.App.HasFile("config/routes.rb") || ctx.App.HasFile("bin/rails") {
		return "rails"
	}
	if ctx.App.HasFileWithContent("Gemfile", "sinatra") {
		return "sinatra"
	}
	return "ruby"
}

func (p *RubyProvider) detectVersion(ctx *provider.DetectContext) string {
	// Check .ruby-version file
	if ctx.App.HasFile(".ruby-version") {
		if content, err := ctx.App.ReadFileString(".ruby-version"); err == nil {
			re := regexp.MustCompile(`(\d+\.\d+)`)
			if matches := re.FindString(content); matches != "" {
				return matches
			}
		}
	}

	// Check Gemfile for ruby version
	if ctx.App.HasFile("Gemfile") {
		if content, err := ctx.App.ReadFileString("Gemfile"); err == nil {
			re := regexp.MustCompile(`ruby\s+["'](\d+\.\d+)`)
			if matches := re.FindStringSubmatch(content); len(matches) > 1 {
				return matches[1]
			}
		}
	}

	return "3"
}
