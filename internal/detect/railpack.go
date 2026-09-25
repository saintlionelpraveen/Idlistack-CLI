package detect

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/config"
	"github.com/idlistack/cli/internal/ui"
)

const DefaultRailpackVersion = "v0.39.0"

// ResolveRailpackBinary locates the railpack binary.
// 1. Checks system PATH
// 2. Checks ~/.idlistack/bin/railpack
// 3. Automatically downloads the standalone static binary into ~/.idlistack/bin/railpack
func ResolveRailpackBinary() (string, error) {
	// 1. Check system PATH
	if bin, err := exec.LookPath("railpack"); err == nil {
		return bin, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	targetDir := filepath.Join(home, ".idlistack", "bin")
	binName := "railpack"
	if runtime.GOOS == "windows" {
		binName = "railpack.exe"
	}
	localBin := filepath.Join(targetDir, binName)

	// 2. Check ~/.idlistack/bin/railpack
	if fi, err := os.Stat(localBin); err == nil && !fi.IsDir() {
		return localBin, nil
	}

	// 3. Auto-download standalone binary transparently
	ui.Detail("Downloading Railpack builder (%s) to %s ...", DefaultRailpackVersion, localBin)
	downloadedBin, err := DownloadRailpack(targetDir, DefaultRailpackVersion)
	if err != nil {
		return "", fmt.Errorf("failed to auto-download railpack: %w", err)
	}

	return downloadedBin, nil
}

// DownloadRailpack downloads and extracts the standalone railpack binary for the host OS and architecture.
func DownloadRailpack(targetDir string, version string) (string, error) {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", err
	}

	var assetName string
	isZip := false

	switch runtime.GOOS {
	case "linux":
		if runtime.GOARCH == "arm64" {
			assetName = fmt.Sprintf("railpack-%s-arm64-unknown-linux-musl.tar.gz", version)
		} else {
			assetName = fmt.Sprintf("railpack-%s-x86_64-unknown-linux-musl.tar.gz", version)
		}
	case "darwin":
		if runtime.GOARCH == "arm64" {
			assetName = fmt.Sprintf("railpack-%s-arm64-apple-darwin.tar.gz", version)
		} else {
			assetName = fmt.Sprintf("railpack-%s-x86_64-apple-darwin.tar.gz", version)
		}
	case "windows":
		isZip = true
		if runtime.GOARCH == "arm64" {
			assetName = fmt.Sprintf("railpack-%s-arm64-pc-windows-msvc.zip", version)
		} else {
			assetName = fmt.Sprintf("railpack-%s-x86_64-pc-windows-msvc.zip", version)
		}
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	downloadURL := fmt.Sprintf("https://github.com/railwayapp/railpack/releases/download/%s/%s", version, assetName)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download from %s: %w", downloadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with HTTP %d from %s", resp.StatusCode, downloadURL)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	binName := "railpack"
	if runtime.GOOS == "windows" {
		binName = "railpack.exe"
	}
	finalBinPath := filepath.Join(targetDir, binName)

	if isZip {
		zipReader, err := zip.NewReader(bytes.NewReader(bodyBytes), int64(len(bodyBytes)))
		if err != nil {
			return "", err
		}
		var found bool
		for _, f := range zipReader.File {
			if f.Name == "railpack.exe" || strings.HasSuffix(f.Name, "/railpack.exe") {
				rc, err := f.Open()
				if err != nil {
					return "", err
				}
				defer rc.Close()

				out, err := os.OpenFile(finalBinPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
				if err != nil {
					return "", err
				}
				defer out.Close()

				if _, err := io.Copy(out, rc); err != nil {
					return "", err
				}
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("railpack.exe not found in zip archive")
		}
	} else {
		// tar.gz
		gzr, err := gzip.NewReader(bytes.NewReader(bodyBytes))
		if err != nil {
			return "", err
		}
		defer gzr.Close()

		tr := tar.NewReader(gzr)
		var found bool
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}

			if filepath.Base(header.Name) == "railpack" && header.Typeflag == tar.TypeReg {
				out, err := os.OpenFile(finalBinPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
				if err != nil {
					return "", err
				}
				if _, err := io.Copy(out, tr); err != nil {
					out.Close()
					return "", err
				}
				out.Close()
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("railpack binary not found in tar.gz archive")
		}
	}

	_ = os.Chmod(finalBinPath, 0755)
	return finalBinPath, nil
}

// DetectWithRailpack executes railpack plan and parses the output into an IdliStack build plan.
// If Railpack binary resolution, execution, or parsing fails or returns an empty plan,
// it gracefully falls back to smart recursive heuristic detection so zero projects are left undetected.
func DetectWithRailpack(ctx context.Context, projectDir string, cfg *config.Config, verbose bool) (*buildplan.Plan, error) {
	bin, err := ResolveRailpackBinary()
	if err == nil {
		cmd := exec.CommandContext(ctx, bin, "plan", projectDir)
		output, cmdErr := cmd.Output()
		if cmdErr == nil {
			plan, parseErr := ParseRailpackPlan(output, projectDir, verbose)
			if parseErr == nil && plan != nil && plan.Provider != "" {
				return plan, nil
			}
		} else if verbose {
			ui.Detail("Railpack plan note: %v", cmdErr)
		}
	} else if verbose {
		ui.Detail("Railpack binary note: %v", err)
	}

	// Smart deep heuristic recursive fallback inspection
	return FallbackFileDetection(projectDir, verbose)
}

// ParseRailpackPlan parses Railpack 0.35+ and 0.39+ JSON output
func ParseRailpackPlan(data []byte, projectDir string, verbose bool) (*buildplan.Plan, error) {
	var rpPlan struct {
		DetectedProviders []string `json:"detectedProviders"`
		ResolvedPackages  map[string]struct {
			Name            string `json:"name"`
			ResolvedVersion string `json:"resolvedVersion"`
		} `json:"resolvedPackages"`
		Deploy struct {
			StartCommand string            `json:"startCommand"`
			Variables    map[string]string `json:"variables"`
		} `json:"deploy"`
		Steps []struct {
			Name      string            `json:"name"`
			Assets    map[string]string `json:"assets"`
			Variables map[string]string `json:"variables"`
			Commands  []struct {
				Cmd        string `json:"cmd"`
				Name       string `json:"name"`
				Path       string `json:"path"`
				CustomName string `json:"customName"`
			} `json:"commands"`
			Inputs []struct {
				Image string `json:"image"`
				Step  string `json:"step"`
			} `json:"inputs"`
		} `json:"steps"`
	}

	if err := json.Unmarshal(data, &rpPlan); err != nil {
		return nil, fmt.Errorf("failed to parse railpack plan: %w", err)
	}

	plan := buildplan.NewDefaultPlan()
	plan.DetectionSource = "railpack"
	plan.DetectionConfidence = "high"
	plan.StartCmd = rpPlan.Deploy.StartCommand
	plan.Port = 0

	// 1. Extract from detectedProviders & resolvedPackages (Railpack 0.35+)
	if len(rpPlan.DetectedProviders) > 0 {
		plan.Provider = rpPlan.DetectedProviders[0]
		plan.DetectedFramework = rpPlan.DetectedProviders[0]
		plan.Stack = rpPlan.DetectedProviders[0]
	}

	for pkgName, pkgInfo := range rpPlan.ResolvedPackages {
		if pkgInfo.ResolvedVersion != "" {
			plan.Runtime = pkgInfo.ResolvedVersion
			plan.StackVersion = pkgInfo.ResolvedVersion
			if plan.Provider == "" {
				plan.Provider = pkgName
				plan.DetectedFramework = pkgName
				plan.Stack = pkgName
			}
			break
		}
	}

	// 2. Parse assets for generated-mise-toml
	for _, step := range rpPlan.Steps {
		if step.Assets != nil {
			if miseToml, ok := step.Assets["generated-mise-toml"]; ok {
				for _, lang := range []string{"python", "node", "go", "rust", "ruby", "php", "java", "elixir", "deno", "bun", "dotnet"} {
					re := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*%s\s*=\s*"([^"]+)"`, regexp.QuoteMeta(lang)))
					if m := re.FindStringSubmatch(miseToml); len(m) > 1 {
						if plan.Provider == "" {
							plan.Provider = lang
							plan.DetectedFramework = lang
							plan.Stack = lang
						}
						if plan.Runtime == "" {
							plan.Runtime = m[1]
							plan.StackVersion = m[1]
						}
						break
					}
				}
			}
		}

		// 3. Check step inputs for container images (Railpack 0.39+ FrankenPHP/PHP, Node, Python, etc.)
		for _, input := range step.Inputs {
			img := strings.ToLower(input.Image)
			if img != "" {
				if strings.Contains(img, "frankenphp") || strings.Contains(img, "php") {
					if plan.Provider == "" {
						plan.Provider = "php"
						plan.Stack = "php"
						plan.DetectedFramework = "frankenphp"
					}
					// Extract PHP version: e.g. php8.4.25 -> 8.4.25 or 8.4
					vRe := regexp.MustCompile(`php(\d+(\.\d+)+)`)
					if vm := vRe.FindStringSubmatch(img); len(vm) > 1 {
						if plan.Runtime == "" {
							plan.Runtime = vm[1]
							plan.StackVersion = vm[1]
						}
					}
				} else if strings.Contains(img, "node") {
					if plan.Provider == "" {
						plan.Provider = "node"
						plan.Stack = "node"
						plan.DetectedFramework = "node"
					}
				} else if strings.Contains(img, "python") {
					if plan.Provider == "" {
						plan.Provider = "python"
						plan.Stack = "python"
						plan.DetectedFramework = "python"
					}
				} else if strings.Contains(img, "golang") || strings.Contains(img, "go:") {
					if plan.Provider == "" {
						plan.Provider = "go"
						plan.Stack = "go"
						plan.DetectedFramework = "go"
					}
				} else if strings.Contains(img, "rust") {
					if plan.Provider == "" {
						plan.Provider = "rust"
						plan.Stack = "rust"
						plan.DetectedFramework = "rust"
					}
				} else if strings.Contains(img, "ruby") {
					if plan.Provider == "" {
						plan.Provider = "ruby"
						plan.Stack = "ruby"
						plan.DetectedFramework = "ruby"
					}
				}
			}
		}

		// 4. Check step variables (e.g. OCTANE_SERVER, PHP_INI_DIR, IS_LARAVEL, etc.)
		for k, v := range step.Variables {
			kl := strings.ToLower(k)
			if strings.Contains(kl, "php") || kl == "octane_server" || kl == "composer_cache_dir" {
				if plan.Provider == "" {
					plan.Provider = "php"
					plan.Stack = "php"
				}
			}
			if kl == "is_laravel" && strings.ToLower(v) == "true" {
				plan.DetectedFramework = "laravel"
			}
			if strings.Contains(kl, "node") || strings.HasPrefix(kl, "npm_") {
				if plan.Provider == "" {
					plan.Provider = "node"
					plan.Stack = "node"
				}
			}
			if strings.Contains(kl, "python") || kl == "poetry_home" || kl == "virtual_env" {
				if plan.Provider == "" {
					plan.Provider = "python"
					plan.Stack = "python"
				}
			}
		}

		// 5. Check step assets (e.g. Caddyfile, php.ini)
		if step.Assets != nil {
			if _, ok := step.Assets["Caddyfile"]; ok {
				if plan.Provider == "" {
					plan.Provider = "php"
					plan.Stack = "php"
				}
			}
			if _, ok := step.Assets["php.ini"]; ok {
				if plan.Provider == "" {
					plan.Provider = "php"
					plan.Stack = "php"
				}
			}
		}

		// 6. Check step names (e.g. install:composer, install:pip)
		if strings.Contains(step.Name, "composer") || strings.Contains(step.Name, "php") {
			if plan.Provider == "" {
				plan.Provider = "php"
				plan.Stack = "php"
			}
		} else if strings.Contains(step.Name, "npm") || strings.Contains(step.Name, "yarn") || strings.Contains(step.Name, "pnpm") {
			if plan.Provider == "" {
				plan.Provider = "node"
				plan.Stack = "node"
			}
		} else if strings.Contains(step.Name, "pip") || strings.Contains(step.Name, "poetry") {
			if plan.Provider == "" {
				plan.Provider = "python"
				plan.Stack = "python"
			}
		}

		if (step.Name == "install" || strings.HasPrefix(step.Name, "install:")) && len(step.Commands) > 0 {
			for _, c := range step.Commands {
				if c.Cmd != "" && plan.InstallCmd == "" {
					plan.InstallCmd = c.Cmd
					break
				}
			}
		}

		if step.Name == "build" && len(step.Commands) > 0 {
			for _, c := range step.Commands {
				if c.Cmd != "" && plan.BuildCmd == "" {
					plan.BuildCmd = c.Cmd
					break
				}
			}
		}
	}

	// 7. Dynamic File-based fallback inspection in projectDir
	if plan.Provider == "" && projectDir != "" {
		if hasFile(projectDir, "composer.json") || hasAnyExtension(projectDir, ".php") {
			plan.Provider = "php"
			plan.Stack = "php"
		} else if hasFile(projectDir, "package.json") {
			plan.Provider = "node"
			plan.Stack = "node"
		} else if hasFile(projectDir, "requirements.txt") || hasFile(projectDir, "pyproject.toml") || hasFile(projectDir, "Pipfile") || hasAnyExtension(projectDir, ".py") {
			plan.Provider = "python"
			plan.Stack = "python"
		} else if hasFile(projectDir, "go.mod") || hasAnyExtension(projectDir, ".go") {
			plan.Provider = "go"
			plan.Stack = "go"
		} else if hasFile(projectDir, "Cargo.toml") || hasAnyExtension(projectDir, ".rs") {
			plan.Provider = "rust"
			plan.Stack = "rust"
		} else if hasFile(projectDir, "Gemfile") || hasAnyExtension(projectDir, ".rb") {
			plan.Provider = "ruby"
			plan.Stack = "ruby"
		} else if hasFile(projectDir, "pom.xml") || hasFile(projectDir, "build.gradle") {
			plan.Provider = "java"
			plan.Stack = "java"
		} else if hasFile(projectDir, "index.html") {
			plan.Provider = "static"
			plan.Stack = "static"
		}
	}

	if plan.Stack == "" && plan.Provider != "" {
		plan.Stack = plan.Provider
	}
	if plan.DetectedFramework == "" && plan.Provider != "" {
		plan.DetectedFramework = plan.Provider
	}

	// Refine detected framework
	if plan.Provider == "php" && projectDir != "" {
		if hasFile(projectDir, "artisan") {
			plan.DetectedFramework = "laravel"
		} else if hasFile(projectDir, "wp-config.php") || hasFile(projectDir, "wp-content") {
			plan.DetectedFramework = "wordpress"
		}
	}

	// 8. Port resolution
	if plan.Port == 0 {
		plan.Port = detectConfigPort(projectDir)
		if plan.Port == 0 {
			// Check if any step variables specify a port
			for _, step := range rpPlan.Steps {
				if sname, ok := step.Variables["SERVER_NAME"]; ok {
					pRe := regexp.MustCompile(`:(\d+)`)
					if pm := pRe.FindStringSubmatch(sname); len(pm) > 1 {
						var p int
						fmt.Sscanf(pm[1], "%d", &p)
						if p > 0 {
							plan.Port = p
							break
						}
					}
				}
				if step.Assets != nil {
					if caddy, ok := step.Assets["Caddyfile"]; ok {
						pRe := regexp.MustCompile(`:\{(?:\$PORT:)?(\d+)\}`)
						if pm := pRe.FindStringSubmatch(caddy); len(pm) > 1 {
							var p int
							fmt.Sscanf(pm[1], "%d", &p)
							if p > 0 {
								plan.Port = p
								break
							}
						}
					}
				}
			}
		}

		if plan.Port == 0 {
			switch plan.Provider {
			case "node":
				plan.Port = 3000
			case "python":
				plan.Port = 8000
			case "go":
				plan.Port = 8080
			case "rust":
				plan.Port = 8080
			case "php":
				plan.Port = 80 // FrankenPHP & Apache default port
			case "static":
				plan.Port = 80 // Nginx default port
			case "java":
				plan.Port = 8080
			case "ruby":
				plan.Port = 3000
			case "elixir":
				plan.Port = 4000
			case "dotnet":
				plan.Port = 5000
			default:
				plan.Port = 8080
			}
		}
	}

	// 9. Inject environment variables from deploy.variables & step.variables
	if plan.Env == nil {
		plan.Env = make(map[string]string)
	}
	if rpPlan.Deploy.Variables != nil {
		for k, v := range rpPlan.Deploy.Variables {
			plan.Env[k] = v
		}
	}
	for _, step := range rpPlan.Steps {
		for k, v := range step.Variables {
			if _, exists := plan.Env[k]; !exists {
				plan.Env[k] = v
			}
		}
	}

	// Auto-infer start command if missing
	if plan.StartCmd == "" && plan.Provider != "" {
		switch plan.Provider {
		case "python":
			if f := findFileRecursive(projectDir, "manage.py", 2); f != "" {
				plan.StartCmd = "python manage.py runserver 0.0.0.0:8000"
				plan.DetectedFramework = "django"
			} else if f := findFileRecursive(projectDir, "app.py", 2); f != "" {
				rel, _ := filepath.Rel(projectDir, f)
				plan.StartCmd = fmt.Sprintf("python %s", filepath.ToSlash(rel))
			} else if f := findFileRecursive(projectDir, "main.py", 2); f != "" {
				rel, _ := filepath.Rel(projectDir, f)
				plan.StartCmd = fmt.Sprintf("python %s", filepath.ToSlash(rel))
			} else if f := findFileRecursive(projectDir, "server.py", 2); f != "" {
				rel, _ := filepath.Rel(projectDir, f)
				plan.StartCmd = fmt.Sprintf("python %s", filepath.ToSlash(rel))
			} else if f := findFirstByExtension(projectDir, ".py", 2); f != "" {
				rel, _ := filepath.Rel(projectDir, f)
				plan.StartCmd = fmt.Sprintf("python %s", filepath.ToSlash(rel))
			} else {
				plan.StartCmd = "python main.py"
			}
		case "node":
			if f := findFileRecursive(projectDir, "server.js", 2); f != "" {
				rel, _ := filepath.Rel(projectDir, f)
				plan.StartCmd = fmt.Sprintf("node %s", filepath.ToSlash(rel))
			} else if f := findFileRecursive(projectDir, "index.js", 2); f != "" {
				rel, _ := filepath.Rel(projectDir, f)
				plan.StartCmd = fmt.Sprintf("node %s", filepath.ToSlash(rel))
			} else if f := findFileRecursive(projectDir, "app.js", 2); f != "" {
				rel, _ := filepath.Rel(projectDir, f)
				plan.StartCmd = fmt.Sprintf("node %s", filepath.ToSlash(rel))
			} else if f := findFirstByExtension(projectDir, ".js", 2); f != "" {
				rel, _ := filepath.Rel(projectDir, f)
				plan.StartCmd = fmt.Sprintf("node %s", filepath.ToSlash(rel))
			} else {
				plan.StartCmd = "node index.js"
			}
		case "php":
			if findFileRecursive(projectDir, "artisan", 2) != "" {
				plan.StartCmd = "php artisan serve --host=0.0.0.0 --port=80"
			} else {
				plan.StartCmd = "apache2-foreground"
			}
		case "static":
			plan.StartCmd = "nginx -g 'daemon off;'"
		}
	}

	if plan.Provider == "" && plan.StartCmd == "" {
		return FallbackFileDetection(projectDir, verbose)
	}

	plan.Normalize()
	return plan, nil
}

func hasFile(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func hasAnyExtension(dir, ext string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			return true
		}
	}
	return false
}

// FallbackFileDetection performs deep recursive inspection across the project directory
// to detect language, framework, start command, and port when Railpack plan is empty or fails.
func FallbackFileDetection(projectDir string, verbose bool) (*buildplan.Plan, error) {
	if projectDir == "" {
		return nil, fmt.Errorf("project directory is empty")
	}

	plan := buildplan.NewDefaultPlan()
	plan.DetectionSource = "heuristic"
	plan.DetectionConfidence = "medium"

	// 1. Check manifests (root and up to 2 subfolder levels)
	pkgJSON := findFileRecursive(projectDir, "package.json", 2)
	reqTxt := findFileRecursive(projectDir, "requirements.txt", 2)
	pyProj := findFileRecursive(projectDir, "pyproject.toml", 2)
	pipfile := findFileRecursive(projectDir, "Pipfile", 2)
	setupPy := findFileRecursive(projectDir, "setup.py", 2)
	compJSON := findFileRecursive(projectDir, "composer.json", 2)
	goMod := findFileRecursive(projectDir, "go.mod", 2)
	cargoToml := findFileRecursive(projectDir, "Cargo.toml", 2)
	indexHTML := findFileRecursive(projectDir, "index.html", 2)
	gemfile := findFileRecursive(projectDir, "Gemfile", 2)
	pomXML := findFileRecursive(projectDir, "pom.xml", 2)

	if pkgJSON != "" {
		plan.Provider = "node"
		plan.Stack = "node"
		plan.Port = 3000
		plan.InstallCmd = "npm install"
		plan.StartCmd = "npm start"

		relPkg, _ := filepath.Rel(projectDir, pkgJSON)
		pkgDir := filepath.Dir(relPkg)

		if data, err := os.ReadFile(pkgJSON); err == nil {
			var pkg struct {
				Main    string            `json:"main"`
				Scripts map[string]string `json:"scripts"`
			}
			if json.Unmarshal(data, &pkg) == nil {
				if pkg.Scripts != nil && pkg.Scripts["start"] != "" {
					plan.StartCmd = "npm start"
				} else if pkg.Main != "" {
					plan.StartCmd = fmt.Sprintf("node %s", pkg.Main)
				}
			}
		}

		if pkgDir != "." && pkgDir != "" {
			plan.InstallCmd = fmt.Sprintf("cd %s && %s", filepath.ToSlash(pkgDir), plan.InstallCmd)
			plan.StartCmd = fmt.Sprintf("cd %s && %s", filepath.ToSlash(pkgDir), plan.StartCmd)
		}
	} else if reqTxt != "" || pyProj != "" || pipfile != "" || setupPy != "" || hasAnyExtensionRecursive(projectDir, ".py", 3) {
		plan.Provider = "python"
		plan.Stack = "python"
		plan.Port = 8000
		if reqTxt != "" {
			relReq, _ := filepath.Rel(projectDir, reqTxt)
			plan.InstallCmd = fmt.Sprintf("pip install -r %s", filepath.ToSlash(relReq))
		}
		if findFileRecursive(projectDir, "manage.py", 2) != "" {
			plan.StartCmd = "python manage.py runserver 0.0.0.0:8000"
			plan.DetectedFramework = "django"
		} else if findFileRecursive(projectDir, "app.py", 2) != "" {
			relApp, _ := filepath.Rel(projectDir, findFileRecursive(projectDir, "app.py", 2))
			plan.StartCmd = fmt.Sprintf("python %s", filepath.ToSlash(relApp))
		} else if findFileRecursive(projectDir, "main.py", 2) != "" {
			relMain, _ := filepath.Rel(projectDir, findFileRecursive(projectDir, "main.py", 2))
			plan.StartCmd = fmt.Sprintf("python %s", filepath.ToSlash(relMain))
		} else if findFileRecursive(projectDir, "server.py", 2) != "" {
			relServer, _ := filepath.Rel(projectDir, findFileRecursive(projectDir, "server.py", 2))
			plan.StartCmd = fmt.Sprintf("python %s", filepath.ToSlash(relServer))
		} else if f := findFirstByExtension(projectDir, ".py", 3); f != "" {
			relPy, _ := filepath.Rel(projectDir, f)
			plan.StartCmd = fmt.Sprintf("python %s", filepath.ToSlash(relPy))
		} else {
			plan.StartCmd = "python main.py"
		}
	} else if compJSON != "" || hasAnyExtensionRecursive(projectDir, ".php", 3) {
		plan.Provider = "php"
		plan.Stack = "php"
		plan.Port = 80
		if findFileRecursive(projectDir, "artisan", 2) != "" {
			plan.DetectedFramework = "laravel"
			plan.StartCmd = "php artisan serve --host=0.0.0.0 --port=80"
		} else {
			plan.StartCmd = "apache2-foreground"
		}
	} else if goMod != "" || hasAnyExtensionRecursive(projectDir, ".go", 3) {
		plan.Provider = "go"
		plan.Stack = "go"
		plan.Port = 8080
		plan.BuildCmd = "go build -o app ."
		plan.StartCmd = "./app"
	} else if cargoToml != "" || hasAnyExtensionRecursive(projectDir, ".rs", 3) {
		plan.Provider = "rust"
		plan.Stack = "rust"
		plan.Port = 8080
		plan.BuildCmd = "cargo build --release"
		plan.StartCmd = "./target/release/app"
	} else if gemfile != "" || hasAnyExtensionRecursive(projectDir, ".rb", 3) {
		plan.Provider = "ruby"
		plan.Stack = "ruby"
		plan.Port = 3000
		plan.StartCmd = "bundle exec rackup -o 0.0.0.0 -p 3000"
	} else if pomXML != "" || hasAnyExtensionRecursive(projectDir, ".java", 3) {
		plan.Provider = "java"
		plan.Stack = "java"
		plan.Port = 8080
		plan.StartCmd = "java -jar target/*.jar"
	} else if hasAnyExtensionRecursive(projectDir, ".js", 3) || hasAnyExtensionRecursive(projectDir, ".ts", 3) || hasAnyExtensionRecursive(projectDir, ".mjs", 3) {
		plan.Provider = "node"
		plan.Stack = "node"
		plan.Port = 3000
		if findFileRecursive(projectDir, "server.js", 2) != "" {
			rel, _ := filepath.Rel(projectDir, findFileRecursive(projectDir, "server.js", 2))
			plan.StartCmd = fmt.Sprintf("node %s", filepath.ToSlash(rel))
		} else if findFileRecursive(projectDir, "index.js", 2) != "" {
			rel, _ := filepath.Rel(projectDir, findFileRecursive(projectDir, "index.js", 2))
			plan.StartCmd = fmt.Sprintf("node %s", filepath.ToSlash(rel))
		} else if findFileRecursive(projectDir, "app.js", 2) != "" {
			rel, _ := filepath.Rel(projectDir, findFileRecursive(projectDir, "app.js", 2))
			plan.StartCmd = fmt.Sprintf("node %s", filepath.ToSlash(rel))
		} else if f := findFirstByExtension(projectDir, ".js", 3); f != "" {
			rel, _ := filepath.Rel(projectDir, f)
			plan.StartCmd = fmt.Sprintf("node %s", filepath.ToSlash(rel))
		} else {
			plan.StartCmd = "node index.js"
		}
	} else if indexHTML != "" || hasAnyExtensionRecursive(projectDir, ".html", 3) || hasAnyExtensionRecursive(projectDir, ".htm", 3) {
		plan.Provider = "static"
		plan.Stack = "static"
		plan.Port = 80
		plan.StartCmd = "nginx -g 'daemon off;'"
	}

	if plan.Provider == "" {
		return nil, fmt.Errorf("no recognizable project files found")
	}

	if verbose {
		ui.Detail("Inferred provider via deep heuristic scan: %s (%s)", plan.Provider, plan.StartCmd)
	}

	plan.Normalize()
	return plan, nil
}

func isIgnoredDir(name string) bool {
	switch name {
	case ".git", ".idlistack", "node_modules", "venv", ".venv", "target", "dist", "__pycache__", ".pytest_cache", ".mypy_cache":
		return true
	default:
		return false
	}
}

func findFileRecursive(root, targetName string, maxDepth int) string {
	var result string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || result != "" {
			return filepath.SkipDir
		}
		if info.IsDir() {
			if isIgnoredDir(info.Name()) {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(root, p)
			if strings.Count(rel, string(os.PathSeparator)) > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == targetName {
			result = p
			return filepath.SkipDir
		}
		return nil
	})
	return result
}

func findFirstByExtension(root, ext string, maxDepth int) string {
	var result string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || result != "" {
			return filepath.SkipDir
		}
		if info.IsDir() {
			if isIgnoredDir(info.Name()) {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(root, p)
			if strings.Count(rel, string(os.PathSeparator)) > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ext) {
			result = p
			return filepath.SkipDir
		}
		return nil
	})
	return result
}

func hasAnyExtensionRecursive(root, ext string, maxDepth int) bool {
	var found bool
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if info.IsDir() {
			if isIgnoredDir(info.Name()) {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(root, p)
			if strings.Count(rel, string(os.PathSeparator)) > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ext) {
			found = true
			return filepath.SkipDir
		}
		return nil
	})
	return found
}
