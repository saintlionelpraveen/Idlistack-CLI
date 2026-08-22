// Package dotnet implements the .NET provider.
// Detects: ASP.NET Core and generic .NET applications via *.csproj / *.fsproj files.
package dotnet

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type DotnetProvider struct {
	projectFile string
	version     string
}

func (p *DotnetProvider) Name() string { return "dotnet" }

func (p *DotnetProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasMatch("*.csproj") || ctx.App.HasMatch("*.fsproj") || ctx.App.HasMatch("*.sln"), nil
}

func (p *DotnetProvider) Initialize(ctx *provider.DetectContext) error {
	// Find first .csproj or .fsproj
	if files, err := ctx.App.FindFiles("*.csproj"); err == nil && len(files) > 0 {
		p.projectFile = files[0]
	} else if files, err := ctx.App.FindFiles("*.fsproj"); err == nil && len(files) > 0 {
		p.projectFile = files[0]
	}
	p.version = p.detectVersion(ctx)
	return nil
}

func (p *DotnetProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "dotnet"
	plan.DetectedFramework = "dotnet"
	plan.Runtime = p.version
	plan.Port = 5000

	plan.InstallCmd = "dotnet restore"
	plan.BuildCmd = "dotnet publish -c Release -o /app/publish"
	plan.StartCmd = "dotnet /app/publish/*.dll"

	// Try to get the DLL name from the project file
	if p.projectFile != "" {
		base := strings.TrimSuffix(p.projectFile, ".csproj")
		base = strings.TrimSuffix(base, ".fsproj")
		// Remove any path prefix
		parts := strings.Split(base, "/")
		dllName := parts[len(parts)-1]
		plan.StartCmd = fmt.Sprintf("dotnet /app/publish/%s.dll", dllName)
	}

	plan.Env = map[string]string{
		"ASPNETCORE_URLS":        "http://+:5000",
		"DOTNET_EnableDiagnostics": "0",
	}

	return plan, nil
}

func (p *DotnetProvider) detectVersion(ctx *provider.DetectContext) string {
	// Check global.json
	if ctx.App.HasFile("global.json") {
		var globalJSON struct {
			SDK struct {
				Version string `json:"version"`
			} `json:"sdk"`
		}
		if err := ctx.App.ReadJSON("global.json", &globalJSON); err == nil && globalJSON.SDK.Version != "" {
			re := regexp.MustCompile(`^(\d+\.\d+)`)
			if m := re.FindString(globalJSON.SDK.Version); m != "" {
				return m
			}
		}
	}

	// Check TargetFramework in project file
	if p.projectFile != "" {
		if content, err := ctx.App.ReadFileString(p.projectFile); err == nil {
			re := regexp.MustCompile(`<TargetFramework>net(\d+\.\d+)</TargetFramework>`)
			if m := re.FindStringSubmatch(content); len(m) > 1 {
				return m[1]
			}
		}
	}

	return "8.0"
}
