package detect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/config"
	"github.com/idlistack/cli/internal/ui"
	"github.com/mattn/go-isatty"
)

func applyConfigOverrides(plan *buildplan.Plan, cfg *config.Config) {
	if cfg == nil {
		return
	}
	if cfg.Build.Provider != "" {
		plan.Provider = cfg.Build.Provider
	}
	if cfg.Build.Runtime != "" {
		plan.Runtime = cfg.Build.Runtime
	}
	if cfg.Build.PreInstallCmd != "" {
		plan.PreInstallCmd = cfg.Build.PreInstallCmd
	}
	if cfg.Build.BuildCmd != "" {
		plan.BuildCmd = cfg.Build.BuildCmd
	}
	if cfg.Build.StartCmd != "" {
		plan.StartCmd = cfg.Build.StartCmd
	}
	if cfg.Deploy.Port != 0 {
		plan.Port = cfg.Deploy.Port
	}
}

// Detect runs the two-layer detection pipeline with optional user selection:
//   Layer 0: Check for existing Dockerfile/docker-compose.yml
//   Layer 1/2: Built-in framework detection
func Detect(ctx context.Context, projectDir string, cfg *config.Config, verbose bool) (*buildplan.Plan, error) {
	// Try Layer 0: Existing Dockerfile / Docker Compose
	layer0Plan, err0 := detectDockerAndCompose(projectDir)

	// Try Layer 1/2: Built-in framework detection
	layer12Plan, err12 := detectOwn(projectDir, cfg)

	// If both strategies detected valid options and no explicit config provider override exists
	if layer0Plan != nil && layer12Plan != nil && (cfg == nil || cfg.Build.Provider == "") {
		if isInteractiveTerminal() {
			fmt.Println()
			ui.Info(fmt.Sprintf("Existing container setup found: %s", color.CyanString(layer0Plan.DockerfilePath)))
			ui.Info(fmt.Sprintf("Detected framework signature:  %s (%s)", color.CyanString(layer12Plan.DetectedFramework), color.CyanString(layer12Plan.Provider)))
			fmt.Println()
			fmt.Println("  Choose build method:")
			fmt.Printf("    [1] Use existing Dockerfile (%s)\n", layer0Plan.DockerfilePath)
			fmt.Printf("    [2] Use IdliStack Zero-Config Buildpack (%s / %s)\n", layer12Plan.DetectedFramework, layer12Plan.Provider)
			fmt.Println()
			fmt.Print("  Select option [1/2] (default 1): ")

			var choice string
			fmt.Scanln(&choice)
			choice = strings.TrimSpace(choice)

			if choice == "2" {
				applyConfigOverrides(layer12Plan, cfg)
				return layer12Plan, nil
			}
		}
	}

	if layer0Plan != nil && err0 == nil {
		applyConfigOverrides(layer0Plan, cfg)
		return layer0Plan, nil
	}

	if layer12Plan != nil && err12 == nil {
		applyConfigOverrides(layer12Plan, cfg)
		return layer12Plan, nil
	}

	return nil, fmt.Errorf("could not detect application type. Create a Dockerfile or ensure your project has a recognizable structure")
}

func isInteractiveTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

// ─── Layer 0: Dockerfile & Docker-Compose Detection ─────────────────────

func detectDockerAndCompose(projectDir string) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	var foundDockerfile string
	var detectedPort int
	var detectedCmd string

	// 1. Check for docker-compose.yml or docker-compose.yaml first
	composeFiles := []string{"docker-compose.yml", "docker-compose.yaml"}
	for _, cf := range composeFiles {
		cfPath := filepath.Join(projectDir, cf)
		if data, err := os.ReadFile(cfPath); err == nil {
			content := string(data)

			// Extract dockerfile path from compose (e.g. dockerfile: docker/Dockerfile)
			dfRe := regexp.MustCompile(`(?i)dockerfile:\s*([^\s\n]+)`)
			if matches := dfRe.FindStringSubmatch(content); len(matches) > 1 {
				dfCandidate := strings.Trim(matches[1], `"'`)
				if _, err := os.Stat(filepath.Join(projectDir, dfCandidate)); err == nil {
					foundDockerfile = dfCandidate
				}
			}

			// Extract port mappings (e.g. "3000:3000" or 8080:8080)
			portRe := regexp.MustCompile(`["']?(\d+):(\d+)["']?`)
			allPortMatches := portRe.FindAllStringSubmatch(content, -1)
			for _, m := range allPortMatches {
				if len(m) > 2 {
					var p int
					fmt.Sscanf(m[2], "%d", &p)
					// Avoid secondary service ports like DB/Redis if possible
					if p != 5432 && p != 6379 && p != 3306 && p != 27017 {
						detectedPort = p
						break
					} else if detectedPort == 0 {
						detectedPort = p
					}
				}
			}

			// Extract command if specified
			cmdRe := regexp.MustCompile(`(?m)^\s*command:\s*(.+)`)
			if m := cmdRe.FindStringSubmatch(content); len(m) > 1 {
				rawCmd := strings.TrimSpace(m[1])
				if strings.HasPrefix(rawCmd, "[") && strings.HasSuffix(rawCmd, "]") {
					var parts []string
					if json.Unmarshal([]byte(rawCmd), &parts) == nil {
						rawCmd = strings.Join(parts, " ")
					}
				}
				detectedCmd = rawCmd
			}
			break
		}
	}

	// 2. Search common Dockerfile paths if not explicitly set by docker-compose
	if foundDockerfile == "" {
		dockerfileCandidates := []string{
			"Dockerfile",
			"docker/Dockerfile",
			"deploy/Dockerfile",
			"build/Dockerfile",
			"Dockerfile.prod",
			"Dockerfile.dev",
		}
		for _, dfCandidate := range dockerfileCandidates {
			if _, err := os.Stat(filepath.Join(projectDir, dfCandidate)); err == nil {
				foundDockerfile = dfCandidate
				break
			}
		}
	}

	// If a Dockerfile exists (from root, subfolder, or compose reference)
	if foundDockerfile != "" {
		plan.Provider = "dockerfile"
		plan.DetectedFramework = "custom"
		plan.DockerfilePath = foundDockerfile
		plan.DetectionSource = "layer0-dockerfile"
		plan.DetectionConfidence = "high"
		if detectedCmd != "" {
			plan.StartCmd = detectedCmd
		} else {
			plan.StartCmd = "(defined in Dockerfile)"
		}

		// Try to read EXPOSE port from Dockerfile if port not yet found
		if data, err := os.ReadFile(filepath.Join(projectDir, foundDockerfile)); err == nil {
			content := string(data)
			exposeRe := regexp.MustCompile(`(?m)^EXPOSE\s+(\d+)`)
			if matches := exposeRe.FindStringSubmatch(content); len(matches) > 1 {
				var expPort int
				fmt.Sscanf(matches[1], "%d", &expPort)
				if detectedPort == 0 {
					detectedPort = expPort
				}
			}
		}

		// Fallback to dynamic port check from config files if port is still default/zero
		if detectedPort == 0 {
			detectedPort = detectConfigPort(projectDir)
		}
		if detectedPort != 0 {
			plan.Port = detectedPort
		}
		return plan, nil
	}

	return nil, fmt.Errorf("no Dockerfile or docker-compose found")
}

// detectConfigPort dynamically extracts port definitions from application config files (.env, config.toml, config.json, etc.)
func detectConfigPort(projectDir string) int {
	// 1. Check config*.toml (e.g. config.toml, config.example.toml, config.local.toml)
	tomlFiles, _ := filepath.Glob(filepath.Join(projectDir, "config*.toml"))
	for _, tf := range tomlFiles {
		if data, err := os.ReadFile(tf); err == nil {
			portRe := regexp.MustCompile(`(?m)^\s*port\s*=\s*(\d+)`)
			if matches := portRe.FindStringSubmatch(string(data)); len(matches) > 1 {
				var p int
				fmt.Sscanf(matches[1], "%d", &p)
				if p > 0 {
					return p
				}
			}
		}
	}

	// 2. Check .env / .env.example
	envFiles := []string{".env", ".env.example", ".env.local"}
	for _, ef := range envFiles {
		if data, err := os.ReadFile(filepath.Join(projectDir, ef)); err == nil {
			portRe := regexp.MustCompile(`(?m)^\s*(?:PORT|SERVER_PORT|APP_PORT)\s*=\s*(\d+)`)
			if matches := portRe.FindStringSubmatch(string(data)); len(matches) > 1 {
				var p int
				fmt.Sscanf(matches[1], "%d", &p)
				if p > 0 {
					return p
				}
			}
		}
	}

	return 0
}

// ─── Layer 1: Railpack Detection ────────────────────────────────────────

func detectWithRailpack(ctx context.Context, projectDir string, cfg *config.Config, verbose bool) (*buildplan.Plan, error) {
	args := []string{"plan"}
	if cfg.Build.BuildCmd != "" {
		args = append(args, "--build-cmd", cfg.Build.BuildCmd)
	}
	if cfg.Build.StartCmd != "" {
		args = append(args, "--start-cmd", cfg.Build.StartCmd)
	}
	args = append(args, projectDir)

	cmd := exec.CommandContext(ctx, "railpack", args...)
	output, err := cmd.Output()
	if err != nil {
		if verbose {
			fmt.Printf("  [debug] railpack plan failed: %v\n", err)
		}
		return nil, fmt.Errorf("railpack detection failed: %w", err)
	}

	// Parse Railpack's plan output
	return parseRailpackPlan(output, projectDir, verbose)
}

// parseRailpackPlan converts Railpack's plan JSON into our build plan format
func parseRailpackPlan(data []byte, projectDir string, verbose bool) (*buildplan.Plan, error) {
	var railpackPlan map[string]interface{}
	if err := json.Unmarshal(data, &railpackPlan); err != nil {
		return nil, fmt.Errorf("failed to parse railpack plan: %w", err)
	}

	plan := buildplan.NewDefaultPlan()
	plan.DetectionSource = "layer1-railpack"
	plan.DetectionConfidence = "high"

	// Extract provider from Railpack's "providers" field
	if providers, ok := railpackPlan["providers"].([]interface{}); ok && len(providers) > 0 {
		if providerMap, ok := providers[0].(map[string]interface{}); ok {
			if name, ok := providerMap["name"].(string); ok {
				plan.Provider = name
			}
		}
	}

	// Extract start command
	if startCmd, ok := railpackPlan["start"].(map[string]interface{}); ok {
		if cmd, ok := startCmd["cmd"].(string); ok {
			plan.StartCmd = cmd
		}
	}

	// Extract metadata
	if meta, ok := railpackPlan["metadata"].(map[string]interface{}); ok {
		if pkgs, ok := meta["packages"].(map[string]interface{}); ok {
			for name, version := range pkgs {
				if strings.Contains(name, "node") {
					plan.Provider = "node"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				} else if strings.Contains(name, "python") {
					plan.Provider = "python"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				} else if strings.Contains(name, "go") {
					plan.Provider = "go"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				} else if strings.Contains(name, "ruby") {
					plan.Provider = "ruby"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				} else if strings.Contains(name, "java") || strings.Contains(name, "jdk") {
					plan.Provider = "java"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				} else if strings.Contains(name, "php") {
					plan.Provider = "php"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				} else if strings.Contains(name, "rust") {
					plan.Provider = "rust"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				} else if strings.Contains(name, "dotnet") || strings.Contains(name, "csharp") {
					plan.Provider = "dotnet"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				} else if strings.Contains(name, "elixir") {
					plan.Provider = "elixir"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				} else if strings.Contains(name, "swift") {
					plan.Provider = "swift"
					if v, ok := version.(string); ok {
						plan.Runtime = v
					}
				}
			}
		}
	}

	// Detect framework from plan
	plan.DetectedFramework = detectFrameworkFromPlan(plan, railpackPlan)

	// Infer common commands if not set
	inferCommands(plan, projectDir)

	if verbose {
		raw, _ := json.MarshalIndent(railpackPlan, "  ", "  ")
		fmt.Printf("  [debug] Raw railpack plan:\n  %s\n", string(raw))
	}

	// If Railpack returned an empty/skeleton plan, fall through to our own detector
	if plan.Provider == "" && plan.StartCmd == "" {
		return nil, fmt.Errorf("railpack returned empty plan, falling through to built-in detector")
	}

	return plan, nil
}

// ─── Layer 2: Own Detector (Fallback) ───────────────────────────────────

func detectOwn(projectDir string, cfg *config.Config) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.DetectionSource = "layer2-builtin"

	// Walk the source tree and match signal files in priority order

	// ── Tier 1: Framework-specific signatures (highest priority) ────
	frameworkDetectors := []struct {
		file      string
		provider  string
		framework string
		port      int
	}{
		// Node.js frameworks
		{"next.config.js", "node", "nextjs", 3000},
		{"next.config.ts", "node", "nextjs", 3000},
		{"next.config.mjs", "node", "nextjs", 3000},
		{"nuxt.config.ts", "node", "nuxt", 3000},
		{"nuxt.config.js", "node", "nuxt", 3000},
		{"remix.config.js", "node", "remix", 3000},
		{"remix.config.ts", "node", "remix", 3000},
		{"svelte.config.js", "node", "sveltekit", 3000},
		{"astro.config.mjs", "node", "astro", 4321},
		{"astro.config.ts", "node", "astro", 4321},
		{"vite.config.ts", "node", "vite", 5173},
		{"vite.config.js", "node", "vite", 5173},
		{"angular.json", "node", "angular", 4200},
		{"gatsby-config.js", "node", "gatsby", 8000},
		{"gatsby-config.ts", "node", "gatsby", 8000},
		{".ember-cli", "node", "ember", 4200},
		{"vue.config.js", "node", "vue", 8080},

		// Python frameworks
		{"manage.py", "python", "django", 8000},
		{"wsgi.py", "python", "django", 8000},
		{"asgi.py", "python", "django", 8000},
		{"app.py", "python", "flask", 5000},
		{"main.py", "python", "fastapi", 8000},
		
		// Frappe / Bench
		{"sites/common_site_config.json", "python", "frappe", 8000},
		{"Procfile", "python", "frappe", 8000},

		// Ghost
		{".ghost-cli", "node", "ghost", 2368},
		{"config.production.json", "node", "ghost", 2368},
		{"config.development.json", "node", "ghost", 2368},

		// Ruby
		{"Gemfile", "ruby", "rails", 3000},
		{"config.ru", "ruby", "rack", 9292},

		// Whatomate (Go CRM & WhatsApp platform)
		{"config.example.toml", "go", "whatomate", 3000},
		{"config.local.toml", "go", "whatomate", 3000},
		{"config.toml", "go", "whatomate", 3000},
		{"cmd/whatomate", "go", "whatomate", 3000},
		{"doc-wiki-whatomate", "go", "whatomate", 3000},

		// Go
		{"go.mod", "go", "go", 8080},

		// Rust
		{"Cargo.toml", "rust", "rust", 8080},

		// Java/JVM
		{"pom.xml", "java", "maven", 8080},
		{"build.gradle", "java", "gradle", 8080},
		{"build.gradle.kts", "java", "gradle-kotlin", 8080},

		// PHP
		{"composer.json", "php", "composer", 8080},
		{"artisan", "php", "laravel", 8000},

		// .NET
		{"*.csproj", "dotnet", "dotnet", 5000},
		{"*.fsproj", "dotnet", "dotnet-fsharp", 5000},

		// Elixir
		{"mix.exs", "elixir", "phoenix", 4000},

		// Deno
		{"deno.json", "deno", "deno", 8000},
		{"deno.jsonc", "deno", "deno", 8000},

		// Bun
		{"bun.lockb", "bun", "bun", 3000},
		{"bunfig.toml", "bun", "bun", 3000},
	}

	for _, fd := range frameworkDetectors {
		if strings.Contains(fd.file, "*") {
			// Glob pattern
			matches, _ := filepath.Glob(filepath.Join(projectDir, fd.file))
			if len(matches) > 0 {
				plan.Provider = fd.provider
				plan.DetectedFramework = fd.framework
				plan.Port = fd.port
				plan.DetectionConfidence = "medium"
				break
			}
		} else {
			if _, err := os.Stat(filepath.Join(projectDir, fd.file)); err == nil {
				plan.Provider = fd.provider
				plan.DetectedFramework = fd.framework
				plan.Port = fd.port
				plan.DetectionConfidence = "medium"
				break
			}
		}
	}

	// If we still haven't detected, check for generic package manager files
	if plan.Provider == "" {
		genericDetectors := []struct {
			file     string
			provider string
		}{
			{"package.json", "node"},
			{"requirements.txt", "python"},
			{"Pipfile", "python"},
			{"pyproject.toml", "python"},
			{"setup.py", "python"},
			{"Gemfile", "ruby"},
		}
		for _, gd := range genericDetectors {
			if _, err := os.Stat(filepath.Join(projectDir, gd.file)); err == nil {
				plan.Provider = gd.provider
				plan.DetectedFramework = gd.provider
				plan.DetectionConfidence = "low"
				break
			}
		}
	}

	// Static-only fallback
	if plan.Provider == "" {
		if _, err := os.Stat(filepath.Join(projectDir, "index.html")); err == nil {
			plan.Provider = "static"
			plan.DetectedFramework = "static"
			plan.Port = 80
			plan.StartCmd = "npx serve -s . -l 80"
			plan.DetectionConfidence = "medium"
			return plan, nil
		}
	}

	if plan.Provider == "" {
		return nil, fmt.Errorf("no recognizable project structure found")
	}

	// Detect runtime version
	detectRuntimeVersion(projectDir, plan)

	// Infer commands
	inferCommands(plan, projectDir)

	// Apply config overrides
	applyConfigOverrides(plan, cfg)

	return plan, nil
}

// ─── Helper Functions ───────────────────────────────────────────────────

func detectFrameworkFromPlan(plan *buildplan.Plan, raw map[string]interface{}) string {
	if plan.DetectedFramework != "" {
		return plan.DetectedFramework
	}
	return plan.Provider
}

func detectRuntimeVersion(projectDir string, plan *buildplan.Plan) {
	switch plan.Provider {
	case "node":
		detectNodeVersion(projectDir, plan)
	case "python":
		detectPythonVersion(projectDir, plan)
	case "go":
		detectGoVersion(projectDir, plan)
	case "ruby":
		detectRubyVersion(projectDir, plan)
	case "java":
		detectJavaVersion(projectDir, plan)
	case "rust":
		detectRustVersion(projectDir, plan)
	case "php":
		detectPHPVersion(projectDir, plan)
	case "elixir":
		detectElixirVersion(projectDir, plan)
	case "dotnet":
		detectDotnetVersion(projectDir, plan)
	}
}

// ─── Per-Language Version Detectors ─────────────────────────────────────

func detectNodeVersion(projectDir string, plan *buildplan.Plan) {
	// Priority 1: .nvmrc (explicit user intent)
	if data, err := os.ReadFile(filepath.Join(projectDir, ".nvmrc")); err == nil {
		if v := cleanNodeVersion(strings.TrimSpace(string(data))); v != "" {
			plan.Runtime = v
			return
		}
	}
	// Priority 2: .node-version
	if data, err := os.ReadFile(filepath.Join(projectDir, ".node-version")); err == nil {
		if v := cleanNodeVersion(strings.TrimSpace(string(data))); v != "" {
			plan.Runtime = v
			return
		}
	}
	// Priority 3: package.json engines.node
	if data, err := os.ReadFile(filepath.Join(projectDir, "package.json")); err == nil {
		var pkg map[string]interface{}
		json.Unmarshal(data, &pkg)
		if engines, ok := pkg["engines"].(map[string]interface{}); ok {
			if node, ok := engines["node"].(string); ok {
				if v := cleanNodeVersion(node); v != "" {
					plan.Runtime = v
					return
				}
			}
		}
	}
	// Priority 4: Framework-specific version files
	//   Ghost: read versions/<ver>/package.json → engines.node
	if plan.DetectedFramework == "ghost" {
		if v := detectGhostNodeVersion(projectDir); v != "" {
			plan.Runtime = v
			return
		}
	}
	// Priority 5: System runtime fallback
	if v := detectSystemVersion("node", `v(\d+)`); v != "" {
		plan.Runtime = v
	}
}

// detectGhostNodeVersion reads the installed Ghost version's package.json
// to find the required Node version from its engines field.
func detectGhostNodeVersion(projectDir string) string {
	// Try "current" symlink first → resolves to versions/<ver>
	currentLink := filepath.Join(projectDir, "current")
	if target, err := os.Readlink(currentLink); err == nil {
		pkgPath := filepath.Join(target, "package.json")
		if !filepath.IsAbs(pkgPath) {
			pkgPath = filepath.Join(projectDir, pkgPath)
		}
		if v := readEnginesNode(pkgPath); v != "" {
			return v
		}
	}
	// Fallback: scan versions/ directory for the latest
	versionsDir := filepath.Join(projectDir, "versions")
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return ""
	}
	// Take the last entry (highest version when sorted alphabetically)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].IsDir() {
			pkgPath := filepath.Join(versionsDir, entries[i].Name(), "package.json")
			if v := readEnginesNode(pkgPath); v != "" {
				return v
			}
		}
	}
	return ""
}

// readEnginesNode reads a package.json and extracts the engines.node value.
func readEnginesNode(pkgPath string) string {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}
	var pkg map[string]interface{}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	if engines, ok := pkg["engines"].(map[string]interface{}); ok {
		if node, ok := engines["node"].(string); ok {
			return cleanNodeVersion(node)
		}
	}
	return ""
}

func detectPythonVersion(projectDir string, plan *buildplan.Plan) {
	// Priority 1: .python-version
	if data, err := os.ReadFile(filepath.Join(projectDir, ".python-version")); err == nil {
		if v := cleanPythonVersion(strings.TrimSpace(string(data))); v != "" {
			plan.Runtime = v
			return
		}
	}
	// Priority 2: runtime.txt (Heroku convention)
	if data, err := os.ReadFile(filepath.Join(projectDir, "runtime.txt")); err == nil {
		if v := cleanPythonVersion(strings.TrimSpace(string(data))); v != "" {
			plan.Runtime = v
			return
		}
	}
	// Priority 3: pyproject.toml → requires-python
	if data, err := os.ReadFile(filepath.Join(projectDir, "pyproject.toml")); err == nil {
		pyVerRe := regexp.MustCompile(`requires-python\s*=\s*"([^"]+)"`)
		if matches := pyVerRe.FindStringSubmatch(string(data)); len(matches) > 1 {
			if v := cleanPythonVersion(matches[1]); v != "" {
				plan.Runtime = v
				return
			}
		}
	}
	// Priority 4: Pipfile → python_version
	if data, err := os.ReadFile(filepath.Join(projectDir, "Pipfile")); err == nil {
		pipVerRe := regexp.MustCompile(`python_version\s*=\s*"([^"]+)"`)
		if matches := pipVerRe.FindStringSubmatch(string(data)); len(matches) > 1 {
			plan.Runtime = matches[1]
			return
		}
	}
	// Priority 5: System runtime fallback
	if v := detectSystemVersion("python3", `(\d+\.\d+)`); v != "" {
		plan.Runtime = v
	}
}

func detectGoVersion(projectDir string, plan *buildplan.Plan) {
	// go.mod → go X.Y
	if data, err := os.ReadFile(filepath.Join(projectDir, "go.mod")); err == nil {
		goVerRe := regexp.MustCompile(`(?m)^go\s+(\d+\.\d+)`)
		if matches := goVerRe.FindStringSubmatch(string(data)); len(matches) > 1 {
			plan.Runtime = matches[1]
			return
		}
	}
	// System fallback
	if v := detectSystemVersion("go", `go(\d+\.\d+)`); v != "" {
		plan.Runtime = v
	}
}

func detectRubyVersion(projectDir string, plan *buildplan.Plan) {
	// Priority 1: .ruby-version
	if data, err := os.ReadFile(filepath.Join(projectDir, ".ruby-version")); err == nil {
		plan.Runtime = strings.TrimSpace(string(data))
		return
	}
	// Priority 2: Gemfile → ruby 'X.Y.Z'
	if data, err := os.ReadFile(filepath.Join(projectDir, "Gemfile")); err == nil {
		rubyVerRe := regexp.MustCompile(`ruby\s+['"]([\d.]+)['"]`)
		if matches := rubyVerRe.FindStringSubmatch(string(data)); len(matches) > 1 {
			plan.Runtime = matches[1]
			return
		}
	}
	// System fallback
	if v := detectSystemVersion("ruby", `(\d+\.\d+)`); v != "" {
		plan.Runtime = v
	}
}

func detectJavaVersion(projectDir string, plan *buildplan.Plan) {
	// Priority 1: .java-version
	if data, err := os.ReadFile(filepath.Join(projectDir, ".java-version")); err == nil {
		plan.Runtime = strings.TrimSpace(string(data))
		return
	}
	// Priority 2: pom.xml → <java.version>
	if data, err := os.ReadFile(filepath.Join(projectDir, "pom.xml")); err == nil {
		javaVerRe := regexp.MustCompile(`<java\.version>(\d+)</java\.version>`)
		if matches := javaVerRe.FindStringSubmatch(string(data)); len(matches) > 1 {
			plan.Runtime = matches[1]
			return
		}
		// Also check <maven.compiler.source>
		compilerRe := regexp.MustCompile(`<maven\.compiler\.source>(\d+)</maven\.compiler\.source>`)
		if matches := compilerRe.FindStringSubmatch(string(data)); len(matches) > 1 {
			plan.Runtime = matches[1]
			return
		}
	}
	// Priority 3: build.gradle → sourceCompatibility
	for _, gradleFile := range []string{"build.gradle", "build.gradle.kts"} {
		if data, err := os.ReadFile(filepath.Join(projectDir, gradleFile)); err == nil {
			gradleVerRe := regexp.MustCompile(`(?:sourceCompatibility|targetCompatibility|languageVersion\.set)\s*[=(]\s*['"]?(\d+)`)
			if matches := gradleVerRe.FindStringSubmatch(string(data)); len(matches) > 1 {
				plan.Runtime = matches[1]
				return
			}
		}
	}
	// System fallback
	if v := detectSystemVersion("java", `version "(\d+)`); v != "" {
		plan.Runtime = v
	}
}

func detectRustVersion(projectDir string, plan *buildplan.Plan) {
	// Priority 1: rust-toolchain.toml → channel
	if data, err := os.ReadFile(filepath.Join(projectDir, "rust-toolchain.toml")); err == nil {
		channelRe := regexp.MustCompile(`channel\s*=\s*"([^"]+)"`)
		if matches := channelRe.FindStringSubmatch(string(data)); len(matches) > 1 {
			plan.Runtime = matches[1]
			return
		}
	}
	// Priority 2: rust-toolchain (plain file)
	if data, err := os.ReadFile(filepath.Join(projectDir, "rust-toolchain")); err == nil {
		plan.Runtime = strings.TrimSpace(string(data))
		return
	}
	// System fallback
	if v := detectSystemVersion("rustc", `(\d+\.\d+)`); v != "" {
		plan.Runtime = v
	}
}

func detectPHPVersion(projectDir string, plan *buildplan.Plan) {
	// Priority 1: composer.json → require.php
	if data, err := os.ReadFile(filepath.Join(projectDir, "composer.json")); err == nil {
		var composer map[string]interface{}
		json.Unmarshal(data, &composer)
		if require, ok := composer["require"].(map[string]interface{}); ok {
			if php, ok := require["php"].(string); ok {
				// Extract major.minor from constraint like "^8.1" or ">=8.2"
				phpVerRe := regexp.MustCompile(`(\d+\.\d+)`)
				if matches := phpVerRe.FindStringSubmatch(php); len(matches) > 1 {
					plan.Runtime = matches[1]
					return
				}
			}
		}
		// Also check config.platform.php
		if config, ok := composer["config"].(map[string]interface{}); ok {
			if platform, ok := config["platform"].(map[string]interface{}); ok {
				if php, ok := platform["php"].(string); ok {
					phpVerRe := regexp.MustCompile(`(\d+\.\d+)`)
					if matches := phpVerRe.FindStringSubmatch(php); len(matches) > 1 {
						plan.Runtime = matches[1]
						return
					}
				}
			}
		}
	}
	// System fallback
	if v := detectSystemVersion("php", `(\d+\.\d+)`); v != "" {
		plan.Runtime = v
	}
}

func detectElixirVersion(projectDir string, plan *buildplan.Plan) {
	// mix.exs → elixir: "~> X.Y"
	if data, err := os.ReadFile(filepath.Join(projectDir, "mix.exs")); err == nil {
		elixirVerRe := regexp.MustCompile(`elixir:\s*"~>\s*(\d+\.\d+)`)
		if matches := elixirVerRe.FindStringSubmatch(string(data)); len(matches) > 1 {
			plan.Runtime = matches[1]
			return
		}
	}
	// .tool-versions (asdf)
	if data, err := os.ReadFile(filepath.Join(projectDir, ".tool-versions")); err == nil {
		elixirRe := regexp.MustCompile(`elixir\s+(\d+\.\d+)`)
		if matches := elixirRe.FindStringSubmatch(string(data)); len(matches) > 1 {
			plan.Runtime = matches[1]
			return
		}
	}
	// System fallback
	if v := detectSystemVersion("elixir", `(\d+\.\d+)`); v != "" {
		plan.Runtime = v
	}
}

func detectDotnetVersion(projectDir string, plan *buildplan.Plan) {
	// global.json → sdk.version
	if data, err := os.ReadFile(filepath.Join(projectDir, "global.json")); err == nil {
		var globalJSON map[string]interface{}
		json.Unmarshal(data, &globalJSON)
		if sdk, ok := globalJSON["sdk"].(map[string]interface{}); ok {
			if version, ok := sdk["version"].(string); ok {
				// Extract major.minor from "8.0.100"
				dotnetVerRe := regexp.MustCompile(`(\d+\.\d+)`)
				if matches := dotnetVerRe.FindStringSubmatch(version); len(matches) > 1 {
					plan.Runtime = matches[1]
					return
				}
			}
		}
	}
	// *.csproj → <TargetFramework>net8.0</TargetFramework>
	csprojFiles, _ := filepath.Glob(filepath.Join(projectDir, "*.csproj"))
	for _, csproj := range csprojFiles {
		if data, err := os.ReadFile(csproj); err == nil {
			tfmRe := regexp.MustCompile(`<TargetFramework>net(\d+\.\d+)</TargetFramework>`)
			if matches := tfmRe.FindStringSubmatch(string(data)); len(matches) > 1 {
				plan.Runtime = matches[1]
				return
			}
		}
	}
	// System fallback
	if v := detectSystemVersion("dotnet", `(\d+\.\d+)`); v != "" {
		plan.Runtime = v
	}
}

// ─── System Version Detection ───────────────────────────────────────────

// detectSystemVersion runs a CLI command (e.g. "node --version") and extracts
// the version using the given regex pattern. Returns "" if the tool isn't installed.
func detectSystemVersion(binary string, pattern string) string {
	cmd := exec.Command(binary, "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(pattern)
	if matches := re.FindStringSubmatch(string(output)); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// cleanPythonVersion extracts a usable Docker-tag-friendly version from Python
// version specifications like "python-3.11.4", "3.12", ">=3.10", etc.
func cleanPythonVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "python-")
	// Strip constraint prefixes
	for _, prefix := range []string{">=", "<=", "^", "~", ">", "<", "="} {
		raw = strings.TrimPrefix(raw, prefix)
	}
	raw = strings.TrimSpace(raw)
	// Extract major.minor
	re := regexp.MustCompile(`(\d+\.\d+)`)
	if matches := re.FindStringSubmatch(raw); len(matches) > 1 {
		return matches[1]
	}
	return raw
}

func inferCommands(plan *buildplan.Plan, projectDir string) {
	switch plan.Provider {
	case "node":
		if plan.DetectedFramework == "ghost" {
			plan.InstallCmd = "npm install -g ghost-cli@latest"
			// Ghost uses absolute symlinks by default, fix it for the container and rebuild native addons for the container's Node.js version
			plan.BuildCmd = "ln -sfn versions/$(ls versions | tail -n 1) current && cd current && npm rebuild better-sqlite3"
			plan.StartCmd = "node current/index.js"
			plan.Port = 2368
			plan.User = "node"
			plan.Env = map[string]string{
				"paths__contentPath":             "/app/content",
				"database__connection__filename": "/app/content/data/ghost-local.db",
				"server__host":                   "0.0.0.0",
			}
		} else {
			plan.InstallCmd = "npm install"
			plan.BuildCmd = "npm run build"
			plan.StartCmd = "npm start"
			if plan.Port == 0 {
				plan.Port = 3000
			}
		}

	case "python":
		if plan.DetectedFramework == "django" {
			plan.InstallCmd = "pip install -r requirements.txt"
			plan.StartCmd = "python manage.py runserver 0.0.0.0:8000"
			plan.Port = 8000
		} else if plan.DetectedFramework == "fastapi" {
			plan.InstallCmd = "pip install -r requirements.txt"
			plan.StartCmd = "uvicorn main:app --host 0.0.0.0 --port 8000"
			plan.Port = 8000
		} else if plan.DetectedFramework == "frappe" {
			plan.PreInstallCmd = "apt-get update && apt-get install -y git curl redis-server mariadb-client npm && npm install -g yarn && pip install frappe-bench"
			plan.InstallCmd = "bench setup requirements"
			plan.BuildCmd = "bench build"
			plan.StartCmd = "bench start"
			plan.Port = 8000
		} else {
			plan.InstallCmd = "pip install -r requirements.txt"
			plan.StartCmd = "python app.py"
			plan.Port = 5000
		}

	case "go":
		if plan.DetectedFramework == "whatomate" {
			if dynPort := detectConfigPort(projectDir); dynPort > 0 {
				plan.Port = dynPort
			} else {
				plan.Port = 3000
			}
			hasFrontend := false
			if _, err := os.Stat(filepath.Join(projectDir, "frontend", "package.json")); err == nil {
				hasFrontend = true
			}
			if hasFrontend {
				plan.PreInstallCmd = "apk add --no-cache nodejs npm git make gcc musl-dev && (test -f config.toml || cp config.example.toml config.toml || true)"
				plan.BuildCmd = "cd frontend && npm install && npm run build && cd .. && CGO_ENABLED=0 go build -o whatomate ./cmd/whatomate"
			} else {
				plan.PreInstallCmd = "(test -f config.toml || cp config.example.toml config.toml || true)"
				plan.BuildCmd = "CGO_ENABLED=0 go build -o whatomate ./cmd/whatomate"
			}
			plan.StartCmd = "./whatomate server"
		} else {
			if dynPort := detectConfigPort(projectDir); dynPort > 0 {
				plan.Port = dynPort
			} else if plan.Port == 0 {
				plan.Port = 8080
			}
			mainPkg := "."
			if _, err := os.Stat(filepath.Join(projectDir, "cmd")); err == nil {
				if entries, err := os.ReadDir(filepath.Join(projectDir, "cmd")); err == nil {
					for _, entry := range entries {
						if entry.IsDir() {
							mainPkg = "./cmd/" + entry.Name()
							break
						}
					}
				}
			}
			plan.BuildCmd = fmt.Sprintf("go build -o app %s", mainPkg)
			plan.StartCmd = "./app"
		}

	case "rust":
		plan.BuildCmd = "cargo build --release"
		plan.StartCmd = "./target/release/app"
		plan.Port = 8080

	case "ruby":
		plan.InstallCmd = "bundle install"
		if plan.DetectedFramework == "rails" {
			plan.StartCmd = "rails server -b 0.0.0.0 -p 3000"
			plan.Port = 3000
		} else {
			plan.StartCmd = "ruby app.rb"
			plan.Port = 9292
		}

	case "java":
		if plan.DetectedFramework == "maven" {
			plan.BuildCmd = "mvn clean package -DskipTests"
			plan.StartCmd = "java -jar target/*.jar"
		} else if strings.Contains(plan.DetectedFramework, "gradle") {
			plan.BuildCmd = "./gradlew build -x test"
			plan.StartCmd = "java -jar build/libs/*.jar"
		}
		plan.Port = 8080

	case "php":
		if plan.DetectedFramework == "laravel" {
			plan.InstallCmd = "composer install"
			plan.StartCmd = "php artisan serve --host=0.0.0.0 --port=8000"
			plan.Port = 8000
		} else {
			plan.InstallCmd = "composer install"
			plan.StartCmd = "php -S 0.0.0.0:8080"
			plan.Port = 8080
		}

	case "elixir":
		plan.InstallCmd = "mix deps.get"
		plan.BuildCmd = "mix compile"
		plan.StartCmd = "mix phx.server"
		plan.Port = 4000

	case "dotnet":
		plan.BuildCmd = "dotnet publish -c Release"
		plan.StartCmd = "dotnet run"
		plan.Port = 5000

	case "deno":
		plan.StartCmd = "deno run --allow-net main.ts"
		plan.Port = 8000

	case "bun":
		plan.InstallCmd = "bun install"
		plan.StartCmd = "bun run start"
		plan.Port = 3000
	}
}

// cleanNodeVersion extracts a usable Docker-tag-friendly version string from
// various Node version specifications (semver ranges, .nvmrc, engines field).
// Examples:
//   "^22.13.0" → "22"
//   ">=20.0.0" → "20"
//   "v18.12.0" → "18"
//   "20.x"     → "20"
//   "20"       → "20"
//   "lts/*"    → "22" (current LTS)
func cleanNodeVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Handle lts aliases — use Docker's own "lts" tag so it stays current
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "lts") {
		return "lts"
	}

	// Strip common prefixes: v, ^, ~, >=, >, =
	stripped := raw
	for _, prefix := range []string{">=", "<=", ">>", "<<", "^", "~", ">", "<", "=", "v"} {
		stripped = strings.TrimPrefix(stripped, prefix)
	}
	stripped = strings.TrimSpace(stripped)

	// If it contains "||", take the first constraint
	if idx := strings.Index(stripped, "||"); idx >= 0 {
		stripped = strings.TrimSpace(stripped[:idx])
		// Re-strip prefixes
		for _, prefix := range []string{">=", "<=", "^", "~", ">", "<", "=", "v"} {
			stripped = strings.TrimPrefix(stripped, prefix)
		}
		stripped = strings.TrimSpace(stripped)
	}

	// Extract just the major version (first number before '.')
	re := regexp.MustCompile(`^(\d+)`)
	if matches := re.FindStringSubmatch(stripped); len(matches) > 1 {
		return matches[1]
	}

	return stripped
}

