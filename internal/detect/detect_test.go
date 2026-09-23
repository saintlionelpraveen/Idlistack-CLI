package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/idlistack/cli/internal/app"
	"github.com/idlistack/cli/internal/provider"
	"github.com/idlistack/cli/internal/providers/registry"
)

// TestProviderDetectionOrder ensures providers are registered in the correct order.
func TestProviderDetectionOrder(t *testing.T) {
	providers := registry.GetProviders()
	expectedOrder := []string{
		"whatomate", "php", "go", "java", "rust", "ruby", "elixir",
		"python", "deno", "dotnet", "node", "static", "shell",
	}

	if len(providers) != len(expectedOrder) {
		t.Fatalf("expected %d providers, got %d", len(expectedOrder), len(providers))
	}

	for i, p := range providers {
		if p.Name() != expectedOrder[i] {
			t.Errorf("provider[%d]: expected %s, got %s", i, expectedOrder[i], p.Name())
		}
	}
}

// TestGetProviderByName ensures we can look up providers by name.
func TestGetProviderByName(t *testing.T) {
	names := []string{"whatomate", "node", "python", "go", "php", "ruby", "rust", "java", "elixir", "deno", "dotnet", "static", "shell"}
	for _, name := range names {
		p := registry.GetProvider(name)
		if p == nil {
			t.Errorf("GetProvider(%q) returned nil", name)
		}
	}

	if p := registry.GetProvider("nonexistent"); p != nil {
		t.Error("GetProvider(nonexistent) should return nil")
	}
}

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

// TestNodeProviderDetectsPackageJSON verifies Node detection via package.json.
func TestNodeProviderDetectsPackageJSON(t *testing.T) {
	dir := createTestProject(t, map[string]string{
		"package.json": `{"name": "test", "scripts": {"start": "node index.js"}}`,
		"index.js":     "console.log('hello')",
	})

	a, _ := app.NewApp(dir)
	ctx := &provider.DetectContext{App: a, ProjectDir: dir}

	p := registry.GetProvider("node")
	matched, err := p.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !matched {
		t.Fatal("Node provider should match a project with package.json")
	}

	if err := p.Initialize(ctx); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	plan, err := p.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	if plan.Provider != "node" {
		t.Errorf("expected provider=node, got %s", plan.Provider)
	}
	if plan.StartCmd == "" {
		t.Error("expected a start command")
	}
}



// TestGoProviderDetectsGoMod verifies Go detection via go.mod.
func TestGoProviderDetectsGoMod(t *testing.T) {
	dir := createTestProject(t, map[string]string{
		"go.mod":  "module example.com/myapp\n\ngo 1.22\n",
		"main.go": "package main\nfunc main() {}",
	})

	a, _ := app.NewApp(dir)
	ctx := &provider.DetectContext{App: a, ProjectDir: dir}

	p := registry.GetProvider("go")
	matched, _ := p.Detect(ctx)
	if !matched {
		t.Fatal("Go provider should match a project with go.mod")
	}

	p.Initialize(ctx)
	plan, _ := p.Plan(ctx)

	if plan.Provider != "go" {
		t.Errorf("expected provider=go, got %s", plan.Provider)
	}
	if plan.Runtime != "1.22" {
		t.Errorf("expected runtime=1.22, got %s", plan.Runtime)
	}
}

// TestPythonDjangoDetection verifies Django detection via manage.py.
func TestPythonDjangoDetection(t *testing.T) {
	dir := createTestProject(t, map[string]string{
		"requirements.txt": "django==4.2\ngunicorn==21.2.0\n",
		"manage.py":        `os.environ.setdefault("DJANGO_SETTINGS_MODULE", "myapp.settings")`,
	})

	a, _ := app.NewApp(dir)
	ctx := &provider.DetectContext{App: a, ProjectDir: dir}

	p := registry.GetProvider("python")
	matched, _ := p.Detect(ctx)
	if !matched {
		t.Fatal("Python provider should match a project with manage.py")
	}

	p.Initialize(ctx)
	plan, _ := p.Plan(ctx)

	if plan.DetectedFramework != "django" {
		t.Errorf("expected framework=django, got %s", plan.DetectedFramework)
	}
	if plan.Port != 8000 {
		t.Errorf("expected port=8000, got %d", plan.Port)
	}
}

// TestPHPBeforeNode ensures PHP wins over Node when both indicators exist.
func TestPHPBeforeNode(t *testing.T) {
	dir := createTestProject(t, map[string]string{
		"composer.json": `{"name": "my/app"}`,
		"package.json":  `{"name": "frontend"}`,
		"index.php":     "<?php echo 'hello'; ?>",
	})

	a, _ := app.NewApp(dir)
	ctx := &provider.DetectContext{App: a, ProjectDir: dir}

	// PHP should be detected first
	for _, p := range registry.GetProviders() {
		matched, _ := p.Detect(ctx)
		if matched {
			if p.Name() != "php" {
				t.Errorf("expected PHP to win, but %s matched first", p.Name())
			}
			break
		}
	}
}
