package detect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// createTestProject creates a temp directory with specified files.
func createTestProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		fullPath := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	return dir
}

// TestParseRailpackPlan ensures Railpack 0.39+ JSON output is correctly converted to a build plan
func TestParseRailpackPlan(t *testing.T) {
	rawJSON := []byte(`{
		"detectedProviders": ["golang"],
		"resolvedPackages": {
			"go": {
				"name": "go",
				"resolvedVersion": "1.26.5"
			}
		},
		"deploy": {
			"startCommand": "./out",
			"variables": {
				"RAILPACK_VERSION": "0.39.0"
			}
		},
		"steps": [
			{
				"name": "install",
				"commands": [{"cmd": "go mod download"}]
			},
			{
				"name": "build",
				"commands": [{"cmd": "go build -o out"}]
			}
		]
	}`)

	plan, err := ParseRailpackPlan(rawJSON, ".", false)
	if err != nil {
		t.Fatalf("ParseRailpackPlan failed: %v", err)
	}

	if plan.Provider != "golang" {
		t.Errorf("expected provider 'golang', got %s", plan.Provider)
	}
	if plan.Runtime != "1.26.5" {
		t.Errorf("expected runtime '1.26.5', got %s", plan.Runtime)
	}
	if plan.StartCmd != "./out" {
		t.Errorf("expected startCmd './out', got %s", plan.StartCmd)
	}
	if plan.InstallCmd != "go mod download" {
		t.Errorf("expected installCmd 'go mod download', got %s", plan.InstallCmd)
	}
	if plan.BuildCmd != "go build -o out" {
		t.Errorf("expected buildCmd 'go build -o out', got %s", plan.BuildCmd)
	}
}

// TestParseRailpackPlan_PHPFrankenPHP ensures Railpack 0.39 PHP/FrankenPHP output is detected with port 80
func TestParseRailpackPlan_PHPFrankenPHP(t *testing.T) {
	rawJSON := []byte(`{
		"deploy": {
			"startCommand": "/start-container.sh",
			"variables": {
				"RAILPACK_VERSION": "0.39.0"
			}
		},
		"steps": [
			{
				"commands": [
					{"cmd": "sh -c 'apt-get update && apt-get install -y ca-certificates git'"}
				],
				"inputs": [
					{"image": "dunglas/frankenphp:php8.4.25-trixie"}
				],
				"name": "packages:image"
			},
			{
				"assets": {
					"Caddyfile": ":{$PORT:80} {\n root * /app\n php_server\n}\n",
					"php.ini": "[PHP]\nengine = On\n",
					"start-container.sh": "#!/bin/bash\n"
				},
				"name": "prepare",
				"variables": {
					"OCTANE_SERVER": "frankenphp",
					"PHP_INI_DIR": "/usr/local/etc/php",
					"SERVER_NAME": ":80"
				}
			}
		]
	}`)

	plan, err := ParseRailpackPlan(rawJSON, ".", false)
	if err != nil {
		t.Fatalf("ParseRailpackPlan failed: %v", err)
	}

	if plan.Provider != "php" {
		t.Errorf("expected provider 'php', got %s", plan.Provider)
	}
	if plan.Stack != "php" {
		t.Errorf("expected stack 'php', got %s", plan.Stack)
	}
	if plan.Runtime != "8.4.25" {
		t.Errorf("expected runtime '8.4.25', got %s", plan.Runtime)
	}
	if plan.Port != 80 {
		t.Errorf("expected port 80, got %d", plan.Port)
	}
	if plan.StartCmd != "/start-container.sh" {
		t.Errorf("expected startCmd '/start-container.sh', got %s", plan.StartCmd)
	}
}

// TestResolveRailpackBinary ensures railpack binary resolution works
func TestResolveRailpackBinary(t *testing.T) {
	bin, err := ResolveRailpackBinary()
	if err != nil {
		t.Fatalf("ResolveRailpackBinary failed: %v", err)
	}
	if bin == "" {
		t.Error("expected non-empty railpack binary path")
	}
}

// TestDetect_Dockerfile verifies that Dockerfile is detected with highest priority
func TestDetect_Dockerfile(t *testing.T) {
	dir := createTestProject(t, map[string]string{
		"Dockerfile": "FROM alpine\nEXPOSE 9000\nCMD [\"./run\"]",
	})

	ctx := context.Background()
	plan, err := Detect(ctx, dir, nil, false)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if plan.Provider != "dockerfile" {
		t.Errorf("expected provider 'dockerfile', got %s", plan.Provider)
	}
	if plan.Port != 9000 {
		t.Errorf("expected port 9000, got %d", plan.Port)
	}
}
