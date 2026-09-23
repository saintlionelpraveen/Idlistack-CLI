// Package golang implements the Go provider.
// Detects: Go applications via go.mod, go.work, or main.go files.
package golang

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type GoProvider struct {
	goVersion  string
	moduleName string
}

func (p *GoProvider) Name() string {
	return "go"
}

func (p *GoProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("go.mod") ||
		ctx.App.HasFile("go.work") ||
		ctx.App.HasFile("main.go"), nil
}

func (p *GoProvider) Initialize(ctx *provider.DetectContext) error {
	p.goVersion = p.detectGoVersion(ctx)
	p.moduleName = p.detectModuleName(ctx)
	return nil
}

func (p *GoProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "go"
	plan.Stack = "Go"
	plan.StackVersion = p.goVersion
	plan.Runtime = p.goVersion
	plan.DetectedFramework = "go"
	plan.Framework = "go"
	plan.Port = 8080

	// Check for common Go web frameworks and versions
	if ctx.App.HasFile("go.mod") {
		if content, err := ctx.App.ReadFileString("go.mod"); err == nil {
			frameworkMap := map[string]string{
				"github.com/gin-gonic/gin": "gin",
				"github.com/labstack/echo": "echo",
				"github.com/gofiber/fiber": "fiber",
				"github.com/gorilla/mux":   "gorilla",
			}
			for mod, name := range frameworkMap {
				if strings.Contains(content, mod) {
					plan.DetectedFramework = name
					plan.Framework = name
					re := regexp.MustCompile(regexp.QuoteMeta(mod) + `\s+v?([\d\.]+)`)
					if m := re.FindStringSubmatch(content); len(m) > 1 {
						plan.FrameworkVersion = m[1]
					}
					break
				}
			}
		}
	}

	// Build command — detect cmd/ directory pattern
	buildTarget := "."
	if ctx.App.HasDir("cmd") {
		// Look for cmd/server, cmd/api, cmd/app, or cmd/<module-name>
		candidates := []string{"cmd/server", "cmd/api", "cmd/app"}
		if p.moduleName != "" {
			parts := strings.Split(p.moduleName, "/")
			candidates = append(candidates, "cmd/"+parts[len(parts)-1])
		}
		for _, c := range candidates {
			if ctx.App.HasDir(c) {
				buildTarget = "./" + c
				break
			}
		}
	}

	plan.InstallCmd = ""
	plan.BuildCmd = fmt.Sprintf("CGO_ENABLED=0 go build -o /app/server %s", buildTarget)
	plan.StartCmd = "/app/server"

	plan.Env = map[string]string{
		"CGO_ENABLED": "0",
		"GOOS":        "linux",
	}

	plan.Normalize()
	return plan, nil
}

func (p *GoProvider) detectGoVersion(ctx *provider.DetectContext) string {
	if ctx.App.HasFile("go.mod") {
		if content, err := ctx.App.ReadFileString("go.mod"); err == nil {
			re := regexp.MustCompile(`^go\s+(\d+\.\d+)`)
			for _, line := range strings.Split(content, "\n") {
				if matches := re.FindStringSubmatch(strings.TrimSpace(line)); len(matches) > 1 {
					return matches[1]
				}
			}
		}
	}
	return "1"
}

func (p *GoProvider) detectModuleName(ctx *provider.DetectContext) string {
	if ctx.App.HasFile("go.mod") {
		if content, err := ctx.App.ReadFileString("go.mod"); err == nil {
			re := regexp.MustCompile(`^module\s+(.+)`)
			for _, line := range strings.Split(content, "\n") {
				if matches := re.FindStringSubmatch(strings.TrimSpace(line)); len(matches) > 1 {
					return matches[1]
				}
			}
		}
	}
	return ""
}
