// Package node implements the Node.js provider with framework sub-detection.
// Detects: Node.js, Next.js, Nuxt, Remix, Astro, Vite, SvelteKit, Ghost CMS,
// Express, Fastify, Nest.js, and more.
package node

import (
	"encoding/json"
	"fmt"
	"os/exec"
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
	packageJSON      *PackageJSON
	packageManager   string // npm, yarn, pnpm, bun
	framework        string // next, nuxt, remix, astro, express, etc.
	frameworkVersion string
	workdir          string // dynamically detected subdirectory (e.g. current, app, server)
}

func (p *NodeProvider) Name() string {
	return "node"
}

// Detect checks for package.json in root or common subdirectories
func (p *NodeProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	if ctx.App.HasFile("package.json") || ctx.App.HasFile("package.json5") {
		return true, nil
	}
	
	subdirs := []string{"current", "app", "server", "backend", "api"}
	for _, dir := range subdirs {
		if ctx.App.HasFile(fmt.Sprintf("%s/package.json", dir)) {
			return true, nil
		}
	}
	
	return false, nil
}

// Initialize parses package.json, detects package manager and framework
func (p *NodeProvider) Initialize(ctx *provider.DetectContext) error {
	p.packageJSON = &PackageJSON{}
	p.workdir = ""

	pkgPath := "package.json"
	if !ctx.App.HasFile(pkgPath) {
		subdirs := []string{"current", "app", "server", "backend", "api"}
		for _, dir := range subdirs {
			if ctx.App.HasFile(fmt.Sprintf("%s/package.json", dir)) {
				pkgPath = fmt.Sprintf("%s/package.json", dir)
				p.workdir = dir
				break
			}
		}
	}

	if ctx.App.HasFile(pkgPath) {
		if err := ctx.App.ReadJSON(pkgPath, p.packageJSON); err != nil {
			return fmt.Errorf("failed to parse %s: %w", pkgPath, err)
		}
	}

	p.packageManager = p.detectPackageManager(ctx.App)
	p.framework = p.detectFramework(ctx.App)
	p.frameworkVersion = p.detectFrameworkVersion()
	return nil
}

func (p *NodeProvider) detectFrameworkVersion() string {
	if p.packageJSON == nil {
		return ""
	}
	depMap := map[string]string{
		"next":      "next",
		"nuxt":      "nuxt",
		"remix":     "@remix-run/react",
		"astro":     "astro",
		"vite":      "vite",
		"sveltekit": "@sveltejs/kit",
		"express":   "express",
		"fastify":   "fastify",
		"nestjs":    "@nestjs/core",
	}
	depName := depMap[p.framework]
	if depName == "" {
		depName = p.framework
	}
	if v, ok := p.packageJSON.Dependencies[depName]; ok {
		return strings.Trim(v, "^~>=< ")
	}
	if v, ok := p.packageJSON.DevDependencies[depName]; ok {
		return strings.Trim(v, "^~>=< ")
	}
	return ""
}

// Plan generates the build plan based on detected framework
func (p *NodeProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "node"
	plan.Stack = "Node.js"
	plan.StackVersion = p.detectNodeVersion()
	plan.Runtime = plan.StackVersion
	plan.DetectedFramework = p.framework
	plan.Framework = p.framework
	plan.FrameworkVersion = p.frameworkVersion

	// Set environment variables
	plan.Env = map[string]string{
		"NODE_ENV": "production",
		"CI":       "true",
	}

	// Framework-specific plans
	switch p.framework {
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
	case "express", "fastify", "nestjs", "koa", "hapi":
		p.planGenericNode(ctx, plan)
	default:
		p.planGenericNode(ctx, plan)
	}

	// Override with package manager specific install command
	plan.InstallCmd = p.getInstallCommand(ctx.App)

	// Build command from scripts
	if plan.BuildCmd == "" && p.packageJSON.HasScript("build") {
		plan.BuildCmd = p.getRunCommand("build")
	}

	// Start command priority: scripts.start → main → index.js
	if plan.StartCmd == "" {
		plan.StartCmd = p.getStartCommand()
	}

	plan.Normalize()
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

	// Fallback to host machine's Node.js version to prevent native module mismatches
	// (like better-sqlite3) when copying node_modules directly.
	cmd := exec.Command("node", "-v")
	if out, err := cmd.Output(); err == nil {
		re := regexp.MustCompile(`v(\d+)`)
		if matches := re.FindStringSubmatch(string(out)); len(matches) > 1 {
			return matches[1]
		}
	}

	return "lts"
}

// ─── Framework-Specific Plan Builders ───────────────────────────────────

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

func (p *NodeProvider) planServerFramework(plan *buildplan.Plan) {
	plan.Port = 3000
	plan.StartCmd = p.getStartCommand()
}

func (p *NodeProvider) planGenericNode(ctx *provider.DetectContext, plan *buildplan.Plan) {
	// Dynamically try to detect a port from common config files before defaulting to 3000
	plan.Port = 3000
	
	// Scan config files dynamically for a port definition
	if p.workdir != "" {
		if content, err := ctx.App.ReadFile(fmt.Sprintf("%s/config.development.json", p.workdir)); err == nil {
			if strings.Contains(string(content), "\"port\":") {
				// Just rudimentary extraction for dynamic purposes
				re := regexp.MustCompile(`"port"\s*:\s*(\d+)`)
				if match := re.FindStringSubmatch(string(content)); len(match) > 1 {
					fmt.Sscanf(match[1], "%d", &plan.Port)
				}
			}
		}
	}
	// Also check root config files just in case
	if content, err := ctx.App.ReadFile("config.development.json"); err == nil {
		re := regexp.MustCompile(`"port"\s*:\s*(\d+)`)
		if match := re.FindStringSubmatch(string(content)); len(match) > 1 {
			fmt.Sscanf(match[1], "%d", &plan.Port)
		}
	}
	
	// If the application is located in a subdirectory (like 'current')
	// dynamically adjust the execution commands to target that directory!
	if p.workdir != "" {
		plan.PreInstallCmd = fmt.Sprintf("cd %s && npm rebuild || true", p.workdir)
		if p.packageJSON.Main != "" {
			plan.StartCmd = fmt.Sprintf("node %s/%s", p.workdir, p.packageJSON.Main)
		} else {
			plan.StartCmd = fmt.Sprintf("node %s/index.js", p.workdir)
		}
		plan.InstallCmd = "" // Assuming it's already installed if nested
	} else {
		plan.StartCmd = p.getStartCommand()
	}
}

// ─── Command Helpers ────────────────────────────────────────────────────

func (p *NodeProvider) getInstallCommand(a *app.App) string {
	if !a.HasFile("package.json") {
		return ""
	}
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

