package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
	"github.com/idlistack/cli/internal/config"
	"github.com/idlistack/cli/internal/ui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current deployment status",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		ui.Error("No idlistack.toml found.")
		return err
	}

	name := strings.ToLower(cfg.Project.Name)
	namespace := fmt.Sprintf("idlistack-%s", name)

	ui.PrintBanner()
	fmt.Printf("  Project:   %s\n", color.CyanString(name))
	fmt.Printf("  Namespace: %s\n\n", namespace)

	deployCmd := exec.CommandContext(cmd.Context(), "kubectl", "get", "deployment", name, "-n", namespace, "-o", "json")
	out, err := deployCmd.Output()
	if err != nil {
		ui.Warn("No active deployment found")
		return nil
	}

	var deploy map[string]interface{}
	json.Unmarshal(out, &deploy)
	st := deploy["status"].(map[string]interface{})
	sp := deploy["spec"].(map[string]interface{})
	ready := 0
	if r, ok := st["readyReplicas"]; ok {
		ready = int(r.(float64))
	}
	desired := int(sp["replicas"].(float64))

	statusText := color.GreenString("● Running")
	if ready == 0 {
		statusText = color.RedString("○ Down")
	} else if ready < desired {
		statusText = color.YellowString("◐ Deploying")
	}
	fmt.Printf("  Status: %s (%d/%d)\n", statusText, ready, desired)

	// Get URL via minikube service
	svcCmd := exec.CommandContext(cmd.Context(), "minikube", "service", name, "-n", namespace, "--url")
	if urlOut, err := svcCmd.Output(); err == nil {
		fmt.Printf("  URL:    %s\n", color.CyanString(strings.TrimSpace(string(urlOut))))
	}

	fmt.Println()
	podsCmd := exec.CommandContext(cmd.Context(), "kubectl", "get", "pods", "-n", namespace, "-l", fmt.Sprintf("app=%s", name))
	podsCmd.Stdout = os.Stdout
	podsCmd.Stderr = os.Stderr
	podsCmd.Run()
	return nil
}
