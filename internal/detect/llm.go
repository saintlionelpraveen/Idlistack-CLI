package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/config"
	"github.com/idlistack/cli/internal/ui"
)

// scanDirectory creates a condensed view of the directory structure to send to the LLM
func scanDirectory(dir string) string {
	var sb strings.Builder
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "." {
			return nil
		}

		if info.IsDir() {
			if rel == "node_modules" || rel == ".git" || rel == ".venv" || rel == "venv" || rel == ".idlistack" {
				return filepath.SkipDir
			}
			return nil
		}
		
		sb.WriteString(rel + "\n")
		return nil
	})
	return sb.String()
}

func detectLLM(ctx context.Context, projectDir string, cfg *config.Config) (*buildplan.Plan, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" && cfg != nil && cfg.AI.ApiKey != "" {
		apiKey = cfg.AI.ApiKey
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set. Cannot use AI fallback")
	}

	ui.Detail("Nixpacks returned empty plan, falling back to %s", color.MagentaString("Gemini AI detection..."))

	dirStructure := scanDirectory(projectDir)
	if len(dirStructure) > 5000 {
		dirStructure = dirStructure[:5000] + "\n... (truncated)"
	}

	prompt := fmt.Sprintf(`You are an expert DevOps engineer and build system architect.
I have a project directory with the following structure:
%s

Please analyze this structure and determine the framework and language.
Output a JSON build plan that can be used to construct a Docker image for it. 
Do not output any markdown formatting, only pure JSON.
The JSON must perfectly match this structure:
{
  "provider": "e.g., node, python, go",
  "detected_framework": "e.g., ghost, frappe, nextjs",
  "port": 8080,
  "pre_install_cmd": "e.g., apt-get update && apt-get install -y something (if needed, runs as root)",
  "install_cmd": "e.g., npm install (runs as user)",
  "build_cmd": "e.g., npm run build (runs as user)",
  "start_cmd": "e.g., node current/index.js",
  "user": "e.g., node (or frappe, etc if specific privileges are needed)",
  "env": {
    "ENV_VAR": "value"
  }
}
If no pre_install, install, or build cmd is needed, leave them empty strings.
Output ONLY the raw JSON string.`, dirStructure)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.1,
		},
	}

	b, _ := json.Marshal(reqBody)
	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent?key=" + apiKey

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty AI response")
	}

	text := geminiResp.Candidates[0].Content.Parts[0].Text
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var aiPlan struct {
		Provider          string            `json:"provider"`
		DetectedFramework string            `json:"detected_framework"`
		Port              int               `json:"port"`
		PreInstallCmd     string            `json:"pre_install_cmd"`
		InstallCmd        string            `json:"install_cmd"`
		BuildCmd          string            `json:"build_cmd"`
		StartCmd          string            `json:"start_cmd"`
		User              string            `json:"user"`
		Env               map[string]string `json:"env"`
	}

	if err := json.Unmarshal([]byte(text), &aiPlan); err != nil {
		return nil, fmt.Errorf("failed to parse AI JSON: %w\nResponse was: %s", err, text)
	}

	plan := buildplan.NewDefaultPlan()
	plan.DetectionSource = "layer2-ai"
	plan.DetectionConfidence = "medium"
	plan.Provider = aiPlan.Provider
	plan.DetectedFramework = aiPlan.DetectedFramework
	plan.Port = aiPlan.Port
	plan.PreInstallCmd = aiPlan.PreInstallCmd
	plan.InstallCmd = aiPlan.InstallCmd
	plan.BuildCmd = aiPlan.BuildCmd
	plan.StartCmd = aiPlan.StartCmd
	plan.User = aiPlan.User
	plan.Env = aiPlan.Env

	return plan, nil
}
