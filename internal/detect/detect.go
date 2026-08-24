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
	"github.com/idlistack/cli/internal/app"
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/config"
	"github.com/idlistack/cli/internal/provider"
	"github.com/idlistack/cli/internal/providers/registry"
	"github.com/idlistack/cli/internal/ui"
	"github.com/mattn/go-isatty"
	"gopkg.in/yaml.v3"
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

// Detect runs the detection pipeline:
//
//	Layer 0: Check for existing Dockerfile/docker-compose.yml
//	Layer 1: Native provider detection (Railpack-style — NEW, primary engine)
//	Layer 2: Nixpacks fallback (for edge cases)
//	Layer 3: Gemini AI fallback (last resort)
//
// When both a Dockerfile and a provider match exist, an interactive
// terminal prompt lets the user choose which build strategy to use.
func Detect(ctx context.Context, projectDir string, cfg *config.Config, verbose bool) (*buildplan.Plan, error) {
	// ─── Layer 0: Existing Dockerfile / Docker Compose ──────────────
	layer0Plan, err0 := detectDockerAndCompose(projectDir)

	// ─── Layer 1: Native Provider Detection (primary engine) ────────
	layer1Plan, providerName, err1 := detectWithProviders(projectDir, cfg, verbose)

	// If Layer 1 found something and there's also a Dockerfile, prompt the user
	if layer0Plan != nil && layer1Plan != nil && (cfg == nil || cfg.Build.Provider == "") {
		if IsInteractiveTerminal() {
			fmt.Println()
			ui.Info(fmt.Sprintf("Existing container setup found: %s", color.CyanString(layer0Plan.DockerfilePath)))
			ui.Info(fmt.Sprintf("Detected framework signature:  %s (%s)", color.CyanString(layer1Plan.DetectedFramework), color.CyanString(layer1Plan.Provider)))
			fmt.Println()
			fmt.Println("  Choose build method:")
			fmt.Printf("    [1] Use existing Dockerfile (%s)\n", layer0Plan.DockerfilePath)
			fmt.Printf("    [2] Use IdliStack Zero-Config Buildpack (%s / %s)\n", layer1Plan.DetectedFramework, layer1Plan.Provider)
			fmt.Println()
			fmt.Print("  Select option [1/2] (default 1): ")

			var choice string
			fmt.Scanln(&choice)
			choice = strings.TrimSpace(choice)

			if choice == "2" {
				applyConfigOverrides(layer1Plan, cfg)
				return layer1Plan, nil
			}
		}
	}

	// ─── Return the best available plan ─────────────────────────────

	// Layer 0: Dockerfile wins by default when present
	if layer0Plan != nil && err0 == nil {
		applyConfigOverrides(layer0Plan, cfg)
		return layer0Plan, nil
	}

	// Layer 1: Native provider detection (our primary engine)
	if layer1Plan != nil && err1 == nil {
		if verbose {
			ui.Detail("Detected by provider: %s", color.CyanString(providerName))
		}
		applyConfigOverrides(layer1Plan, cfg)
		return layer1Plan, nil
	}

	// ─── Layer 2: Nixpacks fallback ─────────────────────────────────
	if verbose {
		ui.Detail("No native provider matched, trying Nixpacks fallback...")
	}
	layer2Plan, err2 := detectWithNixpacks(ctx, projectDir, cfg, verbose)
	if layer2Plan != nil && err2 == nil {
		applyConfigOverrides(layer2Plan, cfg)
		return layer2Plan, nil
	}

	// ─── Layer 3: AI fallback ───────────────────────────────────────
	layer3Plan, err3 := detectLLM(ctx, projectDir, cfg)
	if layer3Plan != nil && err3 == nil {
		applyConfigOverrides(layer3Plan, cfg)
		return layer3Plan, nil
	}

	return nil, fmt.Errorf("could not detect application type. Create a Dockerfile or ensure your project has a recognizable structure")
}

func IsInteractiveTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

// ─── Layer 1: Native Provider Detection (Railpack-style) ────────────────

// detectWithProviders scans the project using all registered providers.
// Returns the plan from the first matching provider, plus the provider name.
func detectWithProviders(projectDir string, cfg *config.Config, verbose bool) (*buildplan.Plan, string, error) {
	// Build the file-system index
	projectApp, err := app.NewApp(projectDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to scan project directory: %w", err)
	}

	detectCtx := &provider.DetectContext{
		App:        projectApp,
		Config:     cfg,
		ProjectDir: projectDir,
		Verbose:    verbose,
	}

	// If user has specified a provider in idlistack.toml, use that directly
	if cfg != nil && cfg.Build.Provider != "" {
		if p := registry.GetProvider(cfg.Build.Provider); p != nil {
			if err := p.Initialize(detectCtx); err == nil {
				if plan, err := p.Plan(detectCtx); err == nil {
					plan.DetectionSource = fmt.Sprintf("provider-%s-config", p.Name())
					plan.DetectionConfidence = "high"
					return plan, p.Name(), nil
				}
			}
		}
	}

	// Run all providers in order — first match wins
	for _, p := range registry.GetProviders() {
		matched, err := p.Detect(detectCtx)
		if err != nil {
			if verbose {
				fmt.Printf("  [debug] Provider %s detect error: %v\n", p.Name(), err)
			}
			continue
		}

		if !matched {
			continue
		}

		if verbose {
			fmt.Printf("  [debug] Provider %s matched, initializing...\n", p.Name())
		}

		if err := p.Initialize(detectCtx); err != nil {
			if verbose {
				fmt.Printf("  [debug] Provider %s init failed: %v\n", p.Name(), err)
			}
			continue
		}

		plan, err := p.Plan(detectCtx)
		if err != nil {
			if verbose {
				fmt.Printf("  [debug] Provider %s plan failed: %v\n", p.Name(), err)
			}
			continue
		}

		plan.DetectionSource = fmt.Sprintf("provider-%s", p.Name())
		plan.DetectionConfidence = "high"

		return plan, p.Name(), nil
	}

	return nil, "", fmt.Errorf("no provider matched the project structure")
}

// ─── Layer 0: Dockerfile & Docker-Compose Detection ─────────────────────

type ComposeConfig struct {
	Services map[string]ComposeService `yaml:"services"`
}

type ComposeService struct {
	Image       string      `yaml:"image"`
	Build       interface{} `yaml:"build"`
	Ports       []string    `yaml:"ports"`
	Environment interface{} `yaml:"environment"`
	Volumes     []string    `yaml:"volumes"`
	Command     interface{} `yaml:"command"`
}

func detectDockerAndCompose(projectDir string) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	var foundDockerfile string
	var detectedPort int
	var detectedCmd string
	var detectedImage string
	var detectedEnv map[string]string
	var detectedVolumes []string

	// 1. Check for docker-compose.yml / compose.yml
	composeFiles := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}
	for _, cf := range composeFiles {
		cfPath := filepath.Join(projectDir, cf)
		if data, err := os.ReadFile(cfPath); err == nil {
			var cfg ComposeConfig
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				continue // If parsing fails, try next file
			}

			// Find primary service. Usually the one with ports, or just the first non-database service
			var primaryService *ComposeService
			for name, svc := range cfg.Services {
				// Very basic heuristic to skip databases if there are multiple services
				if len(cfg.Services) > 1 && (strings.Contains(name, "db") || strings.Contains(name, "redis") || strings.Contains(name, "postgres") || strings.Contains(name, "mysql")) {
					continue
				}
				primaryService = &svc
				break
			}

			if primaryService == nil {
				continue // No valid service found
			}

			detectedImage = primaryService.Image

			// Extract dockerfile path from build context if present
			if primaryService.Build != nil {
				switch b := primaryService.Build.(type) {
				case string:
					// e.g. build: .
					if _, err := os.Stat(filepath.Join(projectDir, "Dockerfile")); err == nil {
						foundDockerfile = "Dockerfile"
					}
				case map[string]interface{}:
					// e.g. build: { context: ., dockerfile: alt.Dockerfile }
					dfCandidate := "Dockerfile"
					if df, ok := b["dockerfile"].(string); ok {
						dfCandidate = df
					}
					if _, err := os.Stat(filepath.Join(projectDir, dfCandidate)); err == nil {
						foundDockerfile = dfCandidate
					}
				}
			}

			// Extract Ports
			for _, portStr := range primaryService.Ports {
				// Port string can be "3000:3000", "8080", etc.
				parts := strings.Split(portStr, ":")
				portToParse := parts[len(parts)-1] // Take container port
				var p int
				if _, err := fmt.Sscanf(portToParse, "%d", &p); err == nil {
					if p != 5432 && p != 6379 && p != 3306 && p != 27017 {
						detectedPort = p
						break
					} else if detectedPort == 0 {
						detectedPort = p
					}
				}
			}

			// Extract Environment
			if primaryService.Environment != nil {
				detectedEnv = make(map[string]string)
				switch e := primaryService.Environment.(type) {
				case map[string]interface{}:
					for k, v := range e {
						detectedEnv[k] = fmt.Sprintf("%v", v)
					}
				case []interface{}:
					for _, v := range e {
						str := fmt.Sprintf("%v", v)
						parts := strings.SplitN(str, "=", 2)
						if len(parts) == 2 {
							detectedEnv[parts[0]] = parts[1]
						} else if len(parts) == 1 {
							// If value is missing, some composes leave it empty or fetch from host
							detectedEnv[parts[0]] = ""
						}
					}
				}
			}

			// Extract Volumes
			if len(primaryService.Volumes) > 0 {
				detectedVolumes = primaryService.Volumes
			}

			// Extract Command
			if primaryService.Command != nil {
				switch c := primaryService.Command.(type) {
				case string:
					detectedCmd = c
				case []interface{}:
					var parts []string
					for _, v := range c {
						parts = append(parts, fmt.Sprintf("%v", v))
					}
					detectedCmd = strings.Join(parts, " ")
				}
			}

			break // Successfully parsed one compose file
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

	// 3a. Compose with a pre-built image (no Dockerfile needed)
	if detectedImage != "" && foundDockerfile == "" {
		plan.Provider = "compose"
		plan.DetectedFramework = "compose"
		plan.ComposeImage = detectedImage
		plan.DetectionSource = "layer0-compose-image"
		plan.DetectionConfidence = "high"
		if detectedCmd != "" {
			plan.StartCmd = detectedCmd
		} else {
			plan.StartCmd = "(defined in image)"
		}
		if detectedPort != 0 {
			plan.Port = detectedPort
		}
		if len(detectedEnv) > 0 {
			plan.Env = detectedEnv
		}
		if len(detectedVolumes) > 0 {
			plan.ComposeVolumes = detectedVolumes
		}
		return plan, nil
	}

	// 3b. Dockerfile exists (from root, subfolder, or compose reference)
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

		if detectedPort == 0 {
			detectedPort = detectConfigPort(projectDir)
		}
		if detectedPort != 0 {
			plan.Port = detectedPort
		}
		if len(detectedEnv) > 0 {
			plan.Env = detectedEnv
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

// ─── Layer 2: Nixpacks Fallback ─────────────────────────────────────────

func detectWithNixpacks(ctx context.Context, projectDir string, cfg *config.Config, verbose bool) (*buildplan.Plan, error) {
	args := []string{"plan", projectDir, "--format", "json"}

	bin := "nixpacks"
	if _, err := exec.LookPath(bin); err != nil {
		bin = "./nixpacks" // fallback to local binary
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	output, err := cmd.Output()
	if err != nil {
		if verbose {
			fmt.Printf("  [debug] nixpacks plan failed: %v\n", err)
		}
		return nil, fmt.Errorf("nixpacks detection failed: %w", err)
	}

	return parseNixpacksPlan(output, projectDir, verbose)
}

// parseNixpacksPlan converts Nixpacks's plan JSON into our build plan format
func parseNixpacksPlan(data []byte, projectDir string, verbose bool) (*buildplan.Plan, error) {
	var nxPlan map[string]interface{}
	if err := json.Unmarshal(data, &nxPlan); err != nil {
		return nil, fmt.Errorf("failed to parse nixpacks plan: %w", err)
	}

	plan := buildplan.NewDefaultPlan()
	plan.DetectionSource = "layer2-nixpacks"
	plan.DetectionConfidence = "high"

	// Provider can often be inferred from variables.NIXPACKS_METADATA
	if vars, ok := nxPlan["variables"].(map[string]interface{}); ok {
		if meta, ok := vars["NIXPACKS_METADATA"].(string); ok {
			plan.Provider = meta
			plan.DetectedFramework = meta
		}

		if plan.Env == nil {
			plan.Env = make(map[string]string)
		}
		for k, v := range vars {
			if valStr, ok := v.(string); ok {
				plan.Env[k] = valStr
			}
		}
	}

	if start, ok := nxPlan["start"].(map[string]interface{}); ok {
		if cmd, ok := start["cmd"].(string); ok {
			plan.StartCmd = cmd
		}
	}

	// Dynamic inference based on phases if provider is empty
	if plan.Provider == "" {
		rawStr, _ := json.Marshal(nxPlan)
		lowerStr := strings.ToLower(string(rawStr))
		if strings.Contains(lowerStr, "npm install") || strings.Contains(lowerStr, "node") {
			plan.Provider = "node"
		} else if strings.Contains(lowerStr, "pip install") || strings.Contains(lowerStr, "python") {
			plan.Provider = "python"
		} else if strings.Contains(lowerStr, "go build") || strings.Contains(lowerStr, "golang") {
			plan.Provider = "go"
		} else if strings.Contains(lowerStr, "cargo build") || strings.Contains(lowerStr, "rust") {
			plan.Provider = "rust"
		} else if strings.Contains(lowerStr, "composer install") || strings.Contains(lowerStr, "php") {
			plan.Provider = "php"
		}
	}

	if plan.DetectedFramework == "" {
		plan.DetectedFramework = plan.Provider
	}

	if plan.Port == 0 {
		plan.Port = detectConfigPort(projectDir)
		if plan.Port == 0 {
			switch plan.Provider {
			case "node":
				plan.Port = 3000
			case "python":
				plan.Port = 5000
			case "go":
				plan.Port = 8080
			case "rust":
				plan.Port = 8080
			case "ruby":
				plan.Port = 9292
			case "java":
				plan.Port = 8080
			case "php":
				plan.Port = 8080
			case "elixir":
				plan.Port = 4000
			case "dotnet":
				plan.Port = 5000
			default:
				plan.Port = 8080
			}
		}
	}

	if verbose {
		raw, _ := json.MarshalIndent(nxPlan, "  ", "  ")
		fmt.Printf("  [debug] Raw nixpacks plan:\n  %s\n", string(raw))
	}

	if plan.Provider == "" && plan.StartCmd == "" {
		return nil, fmt.Errorf("nixpacks returned empty plan")
	}

	return plan, nil
}
