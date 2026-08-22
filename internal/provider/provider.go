// Package provider defines the Provider interface and detection context.
// This follows the Railpack pattern: each language/framework implements
// Detect() → Initialize() → Plan() to generate a build plan.
package provider

import (
	"github.com/idlistack/cli/internal/app"
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/config"
)

// Provider is the interface that all language/framework detectors must implement.
// Inspired by Railpack's Provider interface.
type Provider interface {
	// Name returns the provider identifier (e.g., "node", "python", "go").
	Name() string

	// Detect checks if this provider matches the project by looking at files.
	// Must be fast — only check file existence, not content.
	Detect(ctx *DetectContext) (bool, error)

	// Initialize loads framework-specific configuration (e.g., parse package.json).
	// Called only after Detect() returns true.
	Initialize(ctx *DetectContext) error

	// Plan generates the build plan with install, build, and start commands.
	Plan(ctx *DetectContext) (*buildplan.Plan, error)
}

// DetectContext carries all the information providers need during detection.
type DetectContext struct {
	App        *app.App        // Pre-scanned project file system
	Config     *config.Config  // User's idlistack.toml configuration
	ProjectDir string          // Absolute path to the project directory
	Verbose    bool            // Whether to print debug information
}
