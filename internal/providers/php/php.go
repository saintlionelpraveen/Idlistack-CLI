// Package php implements the PHP provider.
// Detects: Laravel, Symfony, WordPress, and generic PHP apps.
package php

import (
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type PhpProvider struct {
	framework string
}

func (p *PhpProvider) Name() string {
	return "php"
}

func (p *PhpProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("composer.json") ||
		ctx.App.HasFile("index.php") ||
		ctx.App.HasFile("artisan"), nil
}

func (p *PhpProvider) Initialize(ctx *provider.DetectContext) error {
	p.framework = p.detectFramework(ctx)
	return nil
}

func (p *PhpProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "php"
	plan.DetectedFramework = p.framework
	plan.Runtime = "8"

	switch p.framework {
	case "laravel":
		plan.InstallCmd = "composer install --no-dev --optimize-autoloader"
		plan.BuildCmd = "php artisan config:cache && php artisan route:cache && php artisan view:cache"
		plan.StartCmd = "php artisan serve --host=0.0.0.0 --port=8080"
		plan.Port = 8080
		plan.Env = map[string]string{
			"APP_ENV": "production",
		}
	case "symfony":
		plan.InstallCmd = "composer install --no-dev --optimize-autoloader"
		plan.BuildCmd = "php bin/console cache:clear --env=prod"
		plan.StartCmd = "php -S 0.0.0.0:8080 -t public"
		plan.Port = 8080
	case "wordpress":
		plan.InstallCmd = ""
		plan.StartCmd = "php -S 0.0.0.0:8080"
		plan.Port = 8080
	default:
		if ctx.App.HasFile("composer.json") {
			plan.InstallCmd = "composer install"
		}
		plan.StartCmd = "php -S 0.0.0.0:8080"
		plan.Port = 8080
	}

	return plan, nil
}

func (p *PhpProvider) detectFramework(ctx *provider.DetectContext) string {
	if ctx.App.HasFile("artisan") {
		return "laravel"
	}
	if ctx.App.HasFile("bin/console") && ctx.App.HasFile("symfony.lock") {
		return "symfony"
	}
	if ctx.App.HasFile("wp-config.php") || ctx.App.HasFile("wp-settings.php") {
		return "wordpress"
	}
	return "php"
}
