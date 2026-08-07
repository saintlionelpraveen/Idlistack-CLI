package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
	"github.com/idlistack/cli/internal/config"
	"github.com/idlistack/cli/internal/ui"
	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Tear down the current project's deployment",
	Long: `Removes all Kubernetes resources associated with the current project:
  - Deployments
  - Services
  - Ingresses
  - Secrets
  - ConfigMaps
  - The project namespace itself

This does NOT remove the built images from Minikube.`,
	RunE: runDown,
}

var (
	downForce bool
)

func init() {
	downCmd.Flags().BoolVarP(&downForce, "force", "f", false, "Skip confirmation prompt")
}

func runDown(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := config.Load(cwd)
	if err != nil {
		ui.Error("No idlistack.toml found. Is this an IdliStack project?")
		return err
	}

	appName := strings.ToLower(cfg.Project.Name)
	namespace := fmt.Sprintf("idlistack-%s", appName)

	if !downForce {
		ui.Warn(fmt.Sprintf("This will delete ALL resources in namespace %s", color.RedString(namespace)))
		fmt.Print("\nType the project name to confirm: ")
		var confirmation string
		fmt.Scanln(&confirmation)
		if confirmation != cfg.Project.Name {
			ui.Error("Confirmation failed. Aborting.")
			return fmt.Errorf("user cancelled")
		}
	}

	ui.Step(1, 2, "Deleting namespace")

	if err := deleteNamespace(ctx, namespace); err != nil {
		return fmt.Errorf("failed to delete namespace: %w", err)
	}

	ui.Step(2, 2, "Cleaning up local state")

	// Clean deploy lock if exists
	lockPath := fmt.Sprintf("/tmp/idlistack-%s.lock", appName)
	os.Remove(lockPath)

	ui.Success(fmt.Sprintf("Project %s torn down successfully", color.CyanString(cfg.Project.Name)))
	return nil
}

func deleteNamespace(ctx context.Context, namespace string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "delete", "namespace", namespace, "--ignore-not-found")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
