package detect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectWithProviders_GhostCMS(t *testing.T) {
	// Setup a fake Ghost project
	dir := createTestProject(t, map[string]string{
		"package.json":            `{"name": "ghost"}`,
		".ghost-cli":              `{"name": "ghost-local"}`,
		"current":                 "5.96.0",
		"config.development.json": `{"server": {"port": 2368}}`,
	})
	os.MkdirAll(filepath.Join(dir, "versions"), 0755)

	plan, err := Detect(context.Background(), dir, nil, false)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if plan.Provider != "node" {
		t.Errorf("Expected provider 'node', got %s", plan.Provider)
	}

	if plan.DetectedFramework != "ghost" {
		t.Errorf("Expected framework 'ghost', got %s", plan.DetectedFramework)
	}

	if plan.Port != 2368 {
		t.Errorf("Expected port 2368, got %d", plan.Port)
	}
}

func TestDetect_EndToEnd_NoDockerfile(t *testing.T) {
	// Setup a simple Node app (no dockerfile)
	dir := createTestProject(t, map[string]string{
		"package.json": `{"name": "test", "scripts": {"start": "node server.js"}}`,
		"server.js":    "console.log('started');",
	})

	ctx := context.Background()
	plan, err := Detect(ctx, dir, nil, false)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Should be detected by Layer 1 (railpack or provider fallback)
	if plan.DetectionSource != "railpack" && plan.DetectionSource != "provider-node" {
		t.Errorf("Expected DetectionSource 'railpack' or 'provider-node', got %s", plan.DetectionSource)
	}
	if plan.Provider != "node" {
		t.Errorf("Expected Provider 'node', got %s", plan.Provider)
	}
	if plan.StartCmd != "npm run start" && plan.StartCmd != "node server.js" && plan.StartCmd != "npm start" {
		t.Errorf("Expected valid StartCmd, got %s", plan.StartCmd)
	}
}

func TestDetect_FinanceSystem_PythonRawScript(t *testing.T) {
	// Setup a script-only project without requirements.txt or Dockerfile (like raw finance-system)
	dir := createTestProject(t, map[string]string{
		"finance.py": "print('Finance system running')",
	})

	ctx := context.Background()
	plan, err := Detect(ctx, dir, nil, false)
	if err != nil {
		t.Fatalf("Detect failed for raw python script: %v", err)
	}

	if plan.Provider != "python" {
		t.Errorf("Expected Provider 'python', got %s", plan.Provider)
	}
	if plan.StartCmd != "python finance.py" {
		t.Errorf("Expected StartCmd 'python finance.py', got %s", plan.StartCmd)
	}
	if plan.Port != 8000 {
		t.Errorf("Expected default Port 8000, got %d", plan.Port)
	}
}

func TestDetect_FinanceSystem_SubfolderPython(t *testing.T) {
	// Setup a project with backend in subfolder
	dir := createTestProject(t, map[string]string{
		"backend/requirements.txt": "fastapi\nuvicorn",
		"backend/main.py":          "print('FastAPI started')",
	})

	ctx := context.Background()
	plan, err := Detect(ctx, dir, nil, false)
	if err != nil {
		t.Fatalf("Detect failed for subfolder python: %v", err)
	}

	if plan.Provider != "python" {
		t.Errorf("Expected Provider 'python', got %s", plan.Provider)
	}
	if plan.StartCmd != "python backend/main.py" && plan.StartCmd != "python main.py" {
		t.Errorf("Expected valid StartCmd, got %s", plan.StartCmd)
	}
}

func TestDetect_FinanceSystem_SubfolderNode(t *testing.T) {
	// Setup a project with server in subfolder
	dir := createTestProject(t, map[string]string{
		"server/package.json": `{"name": "finance-api", "scripts": {"start": "node index.js"}}`,
		"server/index.js":     "console.log('Finance server running')",
	})

	ctx := context.Background()
	plan, err := Detect(ctx, dir, nil, false)
	if err != nil {
		t.Fatalf("Detect failed for subfolder node: %v", err)
	}

	if plan.Provider != "node" {
		t.Errorf("Expected Provider 'node', got %s", plan.Provider)
	}
	if plan.StartCmd != "cd server && npm start" && plan.StartCmd != "npm start" {
		t.Errorf("Expected valid StartCmd, got %s", plan.StartCmd)
	}
}

