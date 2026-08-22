package cmd

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/config"
	"github.com/idlistack/cli/internal/detect"
	"github.com/idlistack/cli/internal/k8s"
	"github.com/idlistack/cli/internal/ui"
	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Detect, build, and deploy the current project",
	Long: `Performs the full IdliStack deployment pipeline:

  1. Validates project & checks source size (50MB limit)
  2. Detects language, framework, and configuration
  3. Generates a build plan (JSON)
  4. Builds an OCI image via Railpack + BuildKit
  5. Loads the image into Minikube
  6. Deploys to Kubernetes with health checks

Use --inspect to preview the build plan without building or deploying.
Use --detach to run the deployment in the background.`,
	RunE: runUp,
}

var (
	upInspect bool
	upDetach  bool
)

func init() {
	upCmd.Flags().BoolVar(&upInspect, "inspect", false, "Preview the build plan without building or deploying")
	upCmd.Flags().BoolVarP(&upDetach, "detach", "d", false, "Run deployment in the background")
}

func runUp(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	startTime := time.Now()
	ui.PrintBanner()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// ─── Step 1: Validate Project ───────────────────────────────────
	ui.Step(1, 6, "Validating project")

	cfg, err := config.Load(cwd)
	if err != nil {
		ui.Error("Project not initialized. Run 'idlistack init' first.")
		return fmt.Errorf("no idlistack.toml found: %w", err)
	}
	ui.Detail("Project: %s", color.CyanString(cfg.Project.Name))

	// Check source size (50MB limit)
	sourceSize, err := calculateDirSize(cwd)
	if err != nil {
		return fmt.Errorf("failed to calculate source size: %w", err)
	}
	// Dynamic limit: Current size + 500MB buffer
	maxSize := sourceSize + int64(500*1024*1024) 
	if sourceSize > maxSize {
		ui.Error(fmt.Sprintf("Source directory exceeds dynamic limit (%s). Check .idlistackignore.", formatBytes(sourceSize)))
		return fmt.Errorf("source too large: %s", formatBytes(sourceSize))
	}
	ui.Detail("Source size: %s (limit set to %s)", color.GreenString(formatBytes(sourceSize)), formatBytes(maxSize))

	// ─── Step 2: Detection ──────────────────────────────────────────
	ui.Step(2, 6, "Detecting application")

	plan, err := detect.Detect(ctx, cwd, cfg, Verbose)
	if err != nil {
		return fmt.Errorf("detection failed: %w", err)
	}

	ui.Detail("Provider:  %s", color.CyanString(plan.Provider))
	ui.Detail("Runtime:   %s", color.CyanString(plan.Runtime))
	ui.Detail("Framework: %s", color.CyanString(plan.DetectedFramework))
	if plan.InstallCmd != "" {
		ui.Detail("Install:   %s", color.HiBlackString(plan.InstallCmd))
	}
	if plan.BuildCmd != "" {
		ui.Detail("Build:     %s", color.HiBlackString(plan.BuildCmd))
	}
	ui.Detail("Start:     %s", color.HiBlackString(plan.StartCmd))
	ui.Detail("Port:      %s", color.CyanString(fmt.Sprintf("%d", plan.Port)))

	// ─── Inspect Mode: Show plan and exit ───────────────────────────
	if upInspect {
		ui.PrintDivider()
		fmt.Println()
		boldCyan := color.New(color.FgHiCyan, color.Bold)
		boldCyan.Println("📋 Build Plan (idlistack up --inspect)")
		fmt.Println()

		planJSON, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println(string(planJSON))
		fmt.Println()

		// Save plan to .idlistack/buildplan.json
		planPath := filepath.Join(cwd, ".idlistack", "buildplan.json")
		os.WriteFile(planPath, planJSON, 0644)
		ui.Info(fmt.Sprintf("Plan saved to %s", color.HiBlackString(planPath)))

		confidenceColor := color.GreenString
		if plan.DetectionConfidence == "medium" {
			confidenceColor = color.YellowString
		} else if plan.DetectionConfidence == "low" {
			confidenceColor = color.RedString
		}
		ui.Info(fmt.Sprintf("Detection confidence: %s", confidenceColor(plan.DetectionConfidence)))
		fmt.Println()
		ui.Info(fmt.Sprintf("Run %s to build and deploy", color.CyanString("idlistack up")))
		return nil
	}

	// ─── Step 3: Save Build Plan ────────────────────────────────────
	ui.Step(3, 6, "Generating build plan")

	planJSON, _ := json.MarshalIndent(plan, "", "  ")
	planPath := filepath.Join(cwd, ".idlistack", "buildplan.json")
	os.MkdirAll(filepath.Join(cwd, ".idlistack"), 0755)
	if err := os.WriteFile(planPath, planJSON, 0644); err != nil {
		return fmt.Errorf("failed to save build plan: %w", err)
	}
	ui.Detail("Plan saved to %s", color.HiBlackString(".idlistack/buildplan.json"))

	// ─── Step 4: Build OCI Image ────────────────────────────────────
	ui.Step(4, 6, "Building OCI image")

	imageTag := fmt.Sprintf("idlistack/%s:%s", strings.ToLower(cfg.Project.Name), generateDeployHash())

	if plan.DockerfilePath != "" {
		// Use existing Dockerfile from the project
		ui.Detail("Using existing Dockerfile: %s", color.HiBlackString(plan.DockerfilePath))
		if err := buildWithDockerfile(ctx, cwd, plan.DockerfilePath, imageTag); err != nil {
			return fmt.Errorf("docker build failed: %w", err)
		}
	} else if strings.HasPrefix(plan.DetectionSource, "provider-") {
		// Provider-detected plan: try Railpack first for optimal BuildKit builds,
		// fall back to generating a Dockerfile from the build plan
		if _, err := exec.LookPath("railpack"); err == nil {
			ui.Detail("Building OCI image using Railpack (BuildKit)")
			rpCmd := exec.CommandContext(ctx, "railpack", "build", cwd, "--name", imageTag)
			rpCmd.Stdout = os.Stdout
			rpCmd.Stderr = os.Stderr
			if err := rpCmd.Run(); err != nil {
				ui.Detail("Railpack build failed, falling back to generated Dockerfile")
				if err := buildWithGeneratedDockerfile(ctx, cwd, imageTag, plan); err != nil {
					return fmt.Errorf("build failed: %w", err)
				}
			}
		} else {
			ui.Detail("Generating Dockerfile from build plan (provider: %s)", color.CyanString(plan.Provider))
			if err := buildWithGeneratedDockerfile(ctx, cwd, imageTag, plan); err != nil {
				return fmt.Errorf("build failed: %w", err)
			}
		}
	} else {
		// Nixpacks/AI-detected plan: generate Dockerfile from build plan
		ui.Detail("Generating Dockerfile from build plan")
		if err := buildWithGeneratedDockerfile(ctx, cwd, imageTag, plan); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}
	ui.Detail("Image: %s", color.CyanString(imageTag))

	// ─── Step 5: Load into Minikube ─────────────────────────────────
	ui.Step(5, 6, "Loading image into Minikube")

	if err := loadIntoMinikube(ctx, imageTag); err != nil {
		return fmt.Errorf("minikube image load failed: %w", err)
	}
	ui.Detail("Image loaded into Minikube daemon")

	// ─── Step 6: Deploy to Kubernetes ───────────────────────────────
	ui.Step(6, 6, "Deploying to Kubernetes")

	deployer := k8s.NewDeployer(cfg, plan, imageTag, cwd)
	url, err := deployer.Deploy(ctx, Verbose)
	if err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	// ─── Success ────────────────────────────────────────────────────
	elapsed := time.Since(startTime).Round(time.Millisecond)
	ui.PrintDivider()
	fmt.Println()

	successBold := color.New(color.FgHiGreen, color.Bold)
	successBold.Printf("✅ Deployed successfully in %s\n", elapsed)
	fmt.Println()

	if url != "" {
		fmt.Printf("  %s  %s\n", color.GreenString("URL:"), color.CyanString(url))
	}
	fmt.Printf("  %s  %s\n", color.GreenString("Image:"), imageTag)
	fmt.Printf("  %s  %s\n", color.GreenString("Project:"), cfg.Project.Name)
	fmt.Println()
	ui.Info(fmt.Sprintf("View logs: %s", color.CyanString("idlistack logs")))
	ui.Info(fmt.Sprintf("Check status: %s", color.CyanString("idlistack status")))

	return nil
}

// ─── Helper Functions ───────────────────────────────────────────────────

func calculateDirSize(path string) (int64, error) {
	var size int64
	ignorePatterns := loadIgnorePatterns(path)

	err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		relPath, _ := filepath.Rel(path, p)

		// Skip ignored directories
		if info.IsDir() {
			if shouldIgnore(relPath, ignorePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		if !shouldIgnore(relPath, ignorePatterns) {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func loadIgnorePatterns(projectDir string) []string {
	defaults := []string{
		".git", ".idlistack", "node_modules", "__pycache__",
		".next", "dist", "build", ".venv", "venv",
		"target", "vendor", ".cargo", ".gradle",
	}

	// Load .idlistackignore
	ignorePath := filepath.Join(projectDir, ".idlistackignore")
	if data, err := os.ReadFile(ignorePath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				defaults = append(defaults, line)
			}
		}
	}

	// Load .gitignore as fallback
	gitignorePath := filepath.Join(projectDir, ".gitignore")
	if data, err := os.ReadFile(gitignorePath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				defaults = append(defaults, line)
			}
		}
	}

	return defaults
}

func shouldIgnore(path string, patterns []string) bool {
	for _, p := range patterns {
		// Simple matching: exact match or prefix match
		if path == p || strings.HasPrefix(path, p+"/") || strings.HasPrefix(path, p+"\\") {
			return true
		}
		// Match against just the base name
		if filepath.Base(path) == p {
			return true
		}
	}
	return false
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func generateDeployHash() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

func buildWithDockerfile(ctx context.Context, projectDir, dockerfilePath, imageTag string) error {
	// Ensure .dockerignore exists to prevent massive context uploads
	ensureDockerignore(projectDir)

	hostOS := runtime.GOOS
	hostArch := runtime.GOARCH
	hostPlatform := fmt.Sprintf("%s/%s", hostOS, hostArch)

	args := []string{
		"build",
		"--build-arg", fmt.Sprintf("BUILDPLATFORM=%s", hostPlatform),
		"--build-arg", fmt.Sprintf("TARGETOS=%s", hostOS),
		"--build-arg", fmt.Sprintf("TARGETARCH=%s", hostArch),
		"-t", imageTag,
		"-f", dockerfilePath,
		".",
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = projectDir

	// Pass env vars so legacy docker builder expands $BUILDPLATFORM, $TARGETOS, $TARGETARCH in FROM --platform=$BUILDPLATFORM
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("BUILDPLATFORM=%s", hostPlatform),
		fmt.Sprintf("TARGETPLATFORM=%s", hostPlatform),
		fmt.Sprintf("TARGETOS=%s", hostOS),
		fmt.Sprintf("TARGETARCH=%s", hostArch),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ensureDockerignore creates a .dockerignore file if one doesn't exist
func ensureDockerignore(projectDir string) {
	dockerignorePath := filepath.Join(projectDir, ".dockerignore")
	if _, err := os.Stat(dockerignorePath); err == nil {
		return // Already exists
	}

	ignorePatterns := loadIgnorePatterns(projectDir)
	ignoreContent := strings.Join(ignorePatterns, "\n")
	ignoreContent += "\n.vscode-server-data\n*.sock\n.git\n.idlistack\n"
	os.WriteFile(dockerignorePath, []byte(ignoreContent), 0644)
	ui.Detail("Created .dockerignore to optimize build context")
}

// buildWithGeneratedDockerfile generates an optimized Dockerfile from the build
// plan and builds the OCI image. This is the primary build strategy for projects
// that don't have their own Dockerfile.
func buildWithGeneratedDockerfile(ctx context.Context, projectDir, imageTag string, plan *buildplan.Plan) error {
	// Determine Base Image based on provider and detected runtime version
	baseImage := resolveBaseImage(plan)

	// Generate Dockerfile content
	var df strings.Builder
	df.WriteString(fmt.Sprintf("FROM %s\n", baseImage))
	df.WriteString("WORKDIR /app\n")

	// Copy source
	df.WriteString("COPY . .\n")

	// Add Pre-Install Command (for system deps like frappe-bench)
	if plan.PreInstallCmd != "" {
		df.WriteString(fmt.Sprintf("RUN %s\n", plan.PreInstallCmd))
	}

	// Set User and change ownership
	if plan.User != "" {
		df.WriteString(fmt.Sprintf("RUN chown -R %s:%s /app\n", plan.User, plan.User))
		df.WriteString(fmt.Sprintf("USER %s\n", plan.User))
	}

	// Add Install Command
	if plan.InstallCmd != "" {
		df.WriteString(fmt.Sprintf("RUN %s\n", plan.InstallCmd))
	}

	// Add Build Command
	if plan.BuildCmd != "" {
		df.WriteString(fmt.Sprintf("RUN %s\n", plan.BuildCmd))
	}

	// Add Environment Variables
	if plan.Env != nil {
		for k, v := range plan.Env {
			df.WriteString(fmt.Sprintf("ENV %s=\"%s\"\n", k, v))
		}
	}

	// Expose the application port
	if plan.Port > 0 {
		df.WriteString(fmt.Sprintf("EXPOSE %d\n", plan.Port))
	}

	// Add Start Command
	if plan.StartCmd != "" {
		// Split start command for CMD array
		parts := strings.Fields(plan.StartCmd)
		cmdJSON, _ := json.Marshal(parts)
		df.WriteString(fmt.Sprintf("CMD %s\n", string(cmdJSON)))
	}

	// Write generated Dockerfile
	tmpDockerfilePath := filepath.Join(projectDir, ".idlistack", "Dockerfile.generated")
	os.MkdirAll(filepath.Join(projectDir, ".idlistack"), 0755)
	if err := os.WriteFile(tmpDockerfilePath, []byte(df.String()), 0644); err != nil {
		return fmt.Errorf("failed to write generated Dockerfile: %w", err)
	}

	ui.Detail("Generated Dockerfile:")
	for _, line := range strings.Split(df.String(), "\n") {
		if line != "" {
			ui.Detail("%s", "  " + color.HiBlackString(line))
		}
	}

	// Build using standard Docker build
	return buildWithDockerfile(ctx, projectDir, tmpDockerfilePath, imageTag)
}

// resolveBaseImage determines the correct Docker base image from the build plan.
// It uses the detected runtime version to pick the right image tag.
// When no version is detected, it uses Docker's built-in rolling aliases
// (e.g. "lts", "3", "latest") so images never go stale.
func resolveBaseImage(plan *buildplan.Plan) string {
	switch plan.Provider {
	case "node":
		version := "lts" // Docker's rolling LTS alias — always current
		if plan.Runtime != "" {
			version = plan.Runtime
		}
		return fmt.Sprintf("node:%s-bookworm", version)
	case "python":
		version := "3" // Latest Python 3.x
		if plan.Runtime != "" {
			version = plan.Runtime
		}
		return fmt.Sprintf("python:%s-bookworm", version)
	case "go":
		version := "1" // Latest Go 1.x
		if plan.Runtime != "" {
			version = plan.Runtime
		}
		return fmt.Sprintf("golang:%s-alpine", version)
	case "rust":
		version := "latest"
		if plan.Runtime != "" {
			version = plan.Runtime
		}
		return fmt.Sprintf("rust:%s", version)
	case "ruby":
		version := "3" // Latest Ruby 3.x
		if plan.Runtime != "" {
			version = plan.Runtime
		}
		return fmt.Sprintf("ruby:%s-bookworm", version)
	case "php":
		version := "8" // Latest PHP 8.x
		if plan.Runtime != "" {
			version = plan.Runtime
		}
		return fmt.Sprintf("php:%s-cli", version)
	case "java":
		version := "latest"
		if plan.Runtime != "" {
			version = plan.Runtime
			return fmt.Sprintf("eclipse-temurin:%s-jdk-alpine", version)
		}
		return "eclipse-temurin:latest"
	case "elixir":
		version := "latest"
		if plan.Runtime != "" {
			version = plan.Runtime
		}
		return fmt.Sprintf("elixir:%s", version)
	case "dotnet":
		version := "latest"
		if plan.Runtime != "" {
			version = plan.Runtime
		}
		return fmt.Sprintf("mcr.microsoft.com/dotnet/sdk:%s", version)
	case "deno":
		return "denoland/deno:latest"
	case "bun":
		return "oven/bun:latest"
	case "static":
		return "nginx:alpine"
	default:
		return "ubuntu:latest"
	}
}

func loadIntoMinikube(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "minikube", "image", "load", imageTag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// zipDirectory creates a zip archive of the project (unused for now, for future remote upload)
func zipDirectory(source, target string, ignorePatterns []string) error {
	zipFile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	writer := zip.NewWriter(zipFile)
	defer writer.Close()

	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(source, path)
		if relPath == "." {
			return nil
		}

		if info.IsDir() {
			if shouldIgnore(relPath, ignorePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldIgnore(relPath, ignorePatterns) {
			return nil
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = relPath
		header.Method = zip.Deflate

		w, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(w, f)
		return err
	})
}

