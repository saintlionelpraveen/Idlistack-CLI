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
	plan.DetectedFramework = "go"
	plan.Runtime = p.goVersion
	plan.Port = 8080

	// Check for common Go web frameworks
	if ctx.App.HasFile("go.mod") {
		if ctx.App.HasFileWithContent("go.mod", "github.com/gin-gonic/gin") {
			plan.DetectedFramework = "gin"
		} else if ctx.App.HasFileWithContent("go.mod", "github.com/labstack/echo") {
			plan.DetectedFramework = "echo"
		} else if ctx.App.HasFileWithContent("go.mod", "github.com/gofiber/fiber") {
			plan.DetectedFramework = "fiber"
		} else if ctx.App.HasFileWithContent("go.mod", "github.com/gorilla/mux") {
			plan.DetectedFramework = "gorilla"
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
