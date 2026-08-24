package detect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	_ "embed"

	"github.com/idlistack/cli/internal/app"
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/config"
)

type FrameworksConfig struct {
	Frameworks []FrameworkRule `json:"frameworks"`
}

type FrameworkRule struct {
	ID                 string          `json:"id"`
	Provider           string          `json:"provider"`
	Detect             DetectCondition `json:"detect"`
	Port               int             `json:"port"`
	PreInstallCmd      string          `json:"pre_install_cmd,omitempty"`
	InstallCmd         string          `json:"install_cmd,omitempty"`
	BuildCmd           string          `json:"build_cmd,omitempty"`
	StartCmd           string          `json:"start_cmd,omitempty"`
	DockerfileTemplate string          `json:"dockerfile_template,omitempty"`
}

type DetectCondition struct {
	Files        []string `json:"files,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
}

//go:embed frameworks.json
var defaultFrameworksJSON []byte

func detectWithRules(projectDir string, cfg *config.Config, verbose bool) (*buildplan.Plan, error) {
	var data []byte

	// 1. Load the dynamic rules file (can be placed in ~/.idlistack/ or embedded in binary)
	home, _ := os.UserHomeDir()
	rulesPath := filepath.Join(home, ".idlistack", "frameworks.json")
	
	if d, err := os.ReadFile(rulesPath); err == nil {
		data = d
	} else {
		data = defaultFrameworksJSON
	}

	var config FrameworksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse frameworks.json: %w", err)
	}

	projectApp, err := app.NewApp(projectDir)
	if err != nil {
		return nil, err
	}

	// 2. Iterate over rules and check for a match
	for _, fw := range config.Frameworks {
		matched := false

		// Check files
		for _, file := range fw.Detect.Files {
			if projectApp.HasFile(file) {
				matched = true
				break
			}
		}

		// Check dependencies (assuming package.json for now, could be extended)
		if !matched && len(fw.Detect.Dependencies) > 0 {
			if projectApp.HasFile("package.json") {
				type PkgJson struct {
					Dependencies    map[string]string `json:"dependencies"`
					DevDependencies map[string]string `json:"devDependencies"`
				}
				var pkg PkgJson
				if err := projectApp.ReadJSON("package.json", &pkg); err == nil {
					for _, dep := range fw.Detect.Dependencies {
						if _, ok := pkg.Dependencies[dep]; ok {
							matched = true
							break
						}
						if _, ok := pkg.DevDependencies[dep]; ok {
							matched = true
							break
						}
					}
				}
			}
		}

		// 3. If matched, generate plan
		if matched {
			plan := buildplan.NewDefaultPlan()
			plan.Provider = fw.Provider
			plan.DetectedFramework = fw.ID
			plan.Port = fw.Port
			plan.PreInstallCmd = fw.PreInstallCmd
			plan.InstallCmd = fw.InstallCmd
			plan.BuildCmd = fw.BuildCmd
			plan.StartCmd = fw.StartCmd
			plan.DetectionSource = "layer1.5-dynamic-rules"
			plan.DetectionConfidence = "high"

			// Handle custom dockerfile templates if defined in rules
			if fw.DockerfileTemplate != "" {
				version := "latest"
				if fw.ID == "ghost" && projectApp.HasFile(".ghost-cli") {
					type GhostCli struct {
						ActiveVersion string `json:"active-version"`
					}
					var gcli GhostCli
					if err := projectApp.ReadJSON(".ghost-cli", &gcli); err == nil && gcli.ActiveVersion != "" {
						version = gcli.ActiveVersion
					}
				}

				content := strings.ReplaceAll(fw.DockerfileTemplate, "{{version}}", version)
				outDir := filepath.Join(projectDir, ".idlistack")
				os.MkdirAll(outDir, 0755)
				outPath := filepath.Join(outDir, "Dockerfile."+fw.ID)
				os.WriteFile(outPath, []byte(content), 0644)
				plan.DockerfilePath = filepath.Join(".idlistack", "Dockerfile."+fw.ID)
			}

			return plan, nil
		}
	}

	return nil, fmt.Errorf("no dynamic framework rule matched")
}
