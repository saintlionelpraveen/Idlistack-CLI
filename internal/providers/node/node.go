// Package node implements the Node.js provider with framework sub-detection.
// Detects: Node.js, Next.js, Nuxt, Remix, Astro, Vite, SvelteKit, Ghost CMS,
// Express, Fastify, Nest.js, and more.
package node

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/idlistack/cli/internal/app"
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

// PackageJSON represents the relevant fields from package.json
type PackageJSON struct {
	Name            string            `json:"name"`
	Main            string            `json:"main"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Engines         map[string]string `json:"engines"`
	PackageManager  string            `json:"packageManager"`
}

func (p *PackageJSON) HasDependency(name string) bool {
	if _, ok := p.Dependencies[name]; ok {
		return true
	}
	if _, ok := p.DevDependencies[name]; ok {
		return true
	}
	return false
}

func (p *PackageJSON) HasScript(name string) bool {
	_, ok := p.Scripts[name]
	return ok
}

type NodeProvider struct {
	packageJSON    *PackageJSON
	packageManager string // npm, yarn, pnpm, bun
	framework      string // next, nuxt, remix, astro, ghost, express, etc.
}

func (p *NodeProvider) Name() string {
	return "node"
}

// Detect checks for package.json or package.json5
func (p *NodeProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("package.json") || ctx.App.HasFile("package.json5"), nil
}

// Initialize parses package.json, detects package manager and framework
func (p *NodeProvider) Initialize(ctx *provider.DetectContext) error {
	p.packageJSON = &PackageJSON{}

	if ctx.App.HasFile("package.json") {
		if err := ctx.App.ReadJSON("package.json", p.packageJSON); err != nil {
			return fmt.Errorf("failed to parse package.json: %w", err)
		}
	}

	p.packageManager = p.detectPackageManager(ctx.App)
	p.framework = p.detectFramework(ctx.App)
	return nil
}

// Plan generates the build plan based on detected framework
func (p *NodeProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "node"
	plan.DetectedFramework = p.framework
	plan.Runtime = p.detectNodeVersion()

	// Set environment variables
	plan.Env = map[string]string{
		"NODE_ENV": "production",
		"CI":       "true",
	}

	// Framework-specific plans
	switch p.framework {
	case "nextjs":
		p.planNextJS(plan)
	case "nuxt":
		p.planNuxt(plan)
	case "remix":
		p.planRemix(plan)
	case "astro":
		p.planAstro(plan)
	case "vite":
		p.planVite(plan)
	case "sveltekit":
		p.planSvelteKit(plan)
	case "ghost":
		p.planGhost(plan, ctx.App)
	case "express", "fastify", "nestjs", "koa", "hapi":
		p.planServerFramework(plan)
	default:
		p.planGenericNode(plan)
	}

	// Override with package manager specific install command
	plan.InstallCmd = p.getInstallCommand()

	// Build command from scripts
	if plan.BuildCmd == "" && p.packageJSON.HasScript("build") {
		plan.BuildCmd = p.getRunCommand("build")
	}

	// Start command priority: scripts.start → main → index.js
	if plan.StartCmd == "" {
		plan.StartCmd = p.getStartCommand()
	}

	return plan, nil
}

// ─── Package Manager Detection ──────────────────────────────────────────

func (p *NodeProvider) detectPackageManager(a *app.App) string {
	// 1. Check packageManager field in package.json
	if p.packageJSON != nil && p.packageJSON.PackageManager != "" {
		pm := strings.Split(p.packageJSON.PackageManager, "@")[0]
		switch pm {
		case "pnpm":
			return "pnpm"
		case "yarn":
			return "yarn"
		case "bun":
			return "bun"
		default:
			return "npm"
		}
	}

	// 2. File-based detection (lockfiles take priority)
	if a.HasFile("pnpm-lock.yaml") {
		return "pnpm"
	}
	if a.HasFile("bun.lockb") || a.HasFile("bun.lock") {
		return "bun"
	}
	if a.HasFile(".yarnrc.yml") || a.HasFile(".yarnrc.yaml") || a.HasFile("yarn.lock") {
		return "yarn"
	}
	if a.HasFile("package-lock.json") {
		return "npm"
	}

	// 3. Default
	return "npm"
}

// ─── Framework Sub-Detection ────────────────────────────────────────────

func (p *NodeProvider) detectFramework(a *app.App) string {
	if p.packageJSON == nil {
		return "node"
	}

	// Ghost CMS — unique structure
	if a.HasFile(".ghost-cli") || (a.HasFile("current") && a.HasDir("versions")) {
		return "ghost"
	}

	// Next.js
	if p.packageJSON.HasDependency("next") {
		return "nextjs"
	}

	// Nuxt
	if p.packageJSON.HasDependency("nuxt") {
		return "nuxt"
	}

	// Remix
	if p.packageJSON.HasDependency("@remix-run/node") || p.packageJSON.HasDependency("@remix-run/react") {
		return "remix"
	}

	// Astro
	if p.packageJSON.HasDependency("astro") {
		return "astro"
	}

	// SvelteKit
	if p.packageJSON.HasDependency("@sveltejs/kit") {
		return "sveltekit"
	}

	// Vite (without a framework on top)
	if p.packageJSON.HasDependency("vite") && !p.packageJSON.HasDependency("next") && !p.packageJSON.HasDependency("nuxt") {
		return "vite"
	}

	// NestJS
	if p.packageJSON.HasDependency("@nestjs/core") {
		return "nestjs"
	}

	// Express
	if p.packageJSON.HasDependency("express") {
		return "express"
	}

	// Fastify
	if p.packageJSON.HasDependency("fastify") {
		return "fastify"
	}

	// Koa
	if p.packageJSON.HasDependency("koa") {
		return "koa"
	}

	// Hapi
	if p.packageJSON.HasDependency("@hapi/hapi") {
		return "hapi"
	}

	return "node"
}

// ─── Node Version Detection ────────────────────────────────────────────

func (p *NodeProvider) detectNodeVersion() string {
	if p.packageJSON != nil && p.packageJSON.Engines != nil {
		if nodeVersion, ok := p.packageJSON.Engines["node"]; ok {
			// Clean semver constraints: ">=18.0.0" → "18", "^20" → "20"
			re := regexp.MustCompile(`(\d+)`)
			if matches := re.FindString(nodeVersion); matches != "" {
				return matches
			}
		}
	}
	return "lts"
}

// ─── Framework-Specific Plan Builders ───────────────────────────────────

func (p *NodeProvider) planNextJS(plan *buildplan.Plan) {
	plan.BuildCmd = p.getRunCommand("build")
	plan.StartCmd = p.getRunCommand("start")
	plan.Port = 3000
	plan.Env["NEXT_TELEMETRY_DISABLED"] = "1"
}

func (p *NodeProvider) planNuxt(plan *buildplan.Plan) {
	plan.BuildCmd = p.getRunCommand("build")
	plan.StartCmd = "node .output/server/index.mjs"
	plan.Port = 3000
	plan.Env["NITRO_HOST"] = "0.0.0.0"
	plan.Env["NITRO_PORT"] = "3000"
}

func (p *NodeProvider) planRemix(plan *buildplan.Plan) {
	plan.BuildCmd = p.getRunCommand("build")
	plan.StartCmd = p.getRunCommand("start")
	plan.Port = 3000
}

func (p *NodeProvider) planAstro(plan *buildplan.Plan) {
	plan.BuildCmd = p.getRunCommand("build")
	plan.StartCmd = "node ./dist/server/entry.mjs"
	plan.Port = 4321
	plan.Env["HOST"] = "0.0.0.0"
}

func (p *NodeProvider) planVite(plan *buildplan.Plan) {
	plan.BuildCmd = p.getRunCommand("build")
	// Vite SPA — serve with a static server
	plan.StartCmd = "npx serve dist"
	plan.Port = 3000
}

func (p *NodeProvider) planSvelteKit(plan *buildplan.Plan) {
	plan.BuildCmd = p.getRunCommand("build")
	plan.StartCmd = "node build/index.js"
	plan.Port = 3000
	plan.Env["ORIGIN"] = "http://localhost:3000"
}

func (p *NodeProvider) planGhost(plan *buildplan.Plan, a *app.App) {
	plan.Runtime = "18" // Ghost requires Node 18.x
	plan.Port = 2368
	plan.InstallCmd = ""
	plan.BuildCmd = ""
	plan.StartCmd = "node current/index.js"
	plan.Env["NODE_ENV"] = "production"

	// Try to extract port from Ghost config
	configFiles := []string{"config.production.json", "config.development.json"}
	for _, cf := range configFiles {
		if a.HasFile(cf) {
			var ghostConfig map[string]interface{}
			if err := a.ReadJSON(cf, &ghostConfig); err == nil {
				if server, ok := ghostConfig["server"].(map[string]interface{}); ok {
					if port, ok := server["port"].(float64); ok {
						plan.Port = int(port)
					}
				}
			}
			break
		}
	}
}

func (p *NodeProvider) planServerFramework(plan *buildplan.Plan) {
	plan.Port = 3000
	plan.StartCmd = p.getStartCommand()
}

func (p *NodeProvider) planGenericNode(plan *buildplan.Plan) {
	plan.Port = 3000
}

// ─── Command Helpers ────────────────────────────────────────────────────

func (p *NodeProvider) getInstallCommand() string {
	switch p.packageManager {
	case "pnpm":
		return "pnpm install --frozen-lockfile"
	case "yarn":
		return "yarn install --frozen-lockfile"
	case "bun":
		return "bun install --frozen-lockfile"
	default:
		return "npm ci"
	}
}

func (p *NodeProvider) getRunCommand(script string) string {
	switch p.packageManager {
	case "pnpm":
		return fmt.Sprintf("pnpm run %s", script)
	case "yarn":
		return fmt.Sprintf("yarn %s", script)
	case "bun":
		return fmt.Sprintf("bun run %s", script)
	default:
		return fmt.Sprintf("npm run %s", script)
	}
}

func (p *NodeProvider) getStartCommand() string {
	// 1. scripts.start
	if p.packageJSON.HasScript("start") {
		return p.getRunCommand("start")
	}

	// 2. main field
	if p.packageJSON.Main != "" {
		return fmt.Sprintf("node %s", p.packageJSON.Main)
	}

	// 3. Fallback to index.js / index.ts
	return "node index.js"
}

// Serialize package.json to JSON for debugging
func (p *PackageJSON) String() string {
	data, _ := json.MarshalIndent(p, "", "  ")
	return string(data)
}
