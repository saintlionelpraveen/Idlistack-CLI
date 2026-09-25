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
