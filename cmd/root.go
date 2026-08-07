package cmd

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	// Version is set at build time
	Version = "dev"
	// Verbose enables debug output
	Verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "idlistack",
	Short: "IdliStack — Deploy applications with zero configuration",
	Long: fmt.Sprintf(`%s

IdliStack is a production-ready deployment CLI that automatically detects
your application's language, framework, and dependencies — then builds and
deploys it to Kubernetes with zero configuration.

Powered by Railpack for intelligent detection and BuildKit for optimized
OCI image builds.`, color.New(color.FgHiCyan, color.Bold).Sprint("🚀 IdliStack")),
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "Enable verbose output")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(downCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(envCmd)
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the IdliStack CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("idlistack version %s\n", Version)
	},
}
