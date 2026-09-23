package buildplan

// Plan represents the IdliStack build plan — the central artifact of detection.
// This is what gets cached, versioned, and inspected via `idlistack up --inspect`.
type Plan struct {
	PlanVersion         string            `json:"planVersion"`
	Stack               string            `json:"stack,omitempty"`
	StackVersion        string            `json:"stackVersion,omitempty"`
	Framework           string            `json:"framework,omitempty"`
	FrameworkVersion    string            `json:"frameworkVersion,omitempty"`
	Provider            string            `json:"provider"`
	Runtime             string            `json:"runtime"`
	DetectedFramework   string            `json:"detectedFramework"`
	InstallCmd          string            `json:"installCmd,omitempty"`
	PreInstallCmd       string            `json:"preInstallCmd,omitempty"`
	BuildCmd            string            `json:"buildCmd,omitempty"`
	StartCmd            string            `json:"startCmd"`
	Port                int               `json:"port"`
	DockerfilePath      string            `json:"dockerfilePath,omitempty"`
	ComposeImage        string            `json:"composeImage,omitempty"`
	ComposeVolumes      []string          `json:"composeVolumes,omitempty"`
	Volumes             []string          `json:"volumes,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`
	HealthCheck         HealthCheck       `json:"healthCheck"`
	Resources           Resources         `json:"resources"`
	PreDeployCmd        string            `json:"preDeployCmd,omitempty"`
	StaticDir           string            `json:"staticDir,omitempty"`
	DetectionConfidence string            `json:"detectionConfidence"`
	DetectionSource     string            `json:"detectionSource"`
	Dependencies        []string          `json:"dependencies,omitempty"`
	User                string            `json:"user,omitempty"`
}

// Normalize ensures Stack, Runtime, and Framework fields are consistently populated
func (p *Plan) Normalize() {
	if p.Stack == "" {
		switch p.Provider {
		case "python":
			p.Stack = "Python"
		case "node":
			p.Stack = "Node.js"
		case "go":
			p.Stack = "Go"
		case "rust":
			p.Stack = "Rust"
		case "ruby":
			p.Stack = "Ruby"
		case "php":
			p.Stack = "PHP"
		case "java":
			p.Stack = "Java"
		case "elixir":
			p.Stack = "Elixir"
		case "dotnet":
			p.Stack = ".NET"
		case "deno":
			p.Stack = "Deno"
		case "bun":
			p.Stack = "Bun"
		case "static":
			p.Stack = "Static"
		default:
			if p.Provider != "" {
				p.Stack = p.Provider
			}
		}
	}
	if p.StackVersion == "" && p.Runtime != "" {
		p.StackVersion = p.Runtime
	}
	if p.Runtime == "" && p.StackVersion != "" {
		p.Runtime = p.StackVersion
	}
	if p.Framework == "" && p.DetectedFramework != "" {
		p.Framework = p.DetectedFramework
	}
	if p.DetectedFramework == "" && p.Framework != "" {
		p.DetectedFramework = p.Framework
	}
}

// HealthCheck configures the liveness/readiness probe
type HealthCheck struct {
	Path     string `json:"path"`
	Interval int    `json:"interval"`
	Timeout  int    `json:"timeout"`
}

// Resources defines the container resource limits
type Resources struct {
	Memory string `json:"memory"`
	CPU    string `json:"cpu"`
}

// NewDefaultPlan creates a plan with sensible defaults
func NewDefaultPlan() *Plan {
	return &Plan{
		PlanVersion:         "1",
		Port:                3000,
		DetectionConfidence: "low",
		HealthCheck: HealthCheck{
			Path:     "/health",
			Interval: 30,
			Timeout:  5,
		},
		Resources: Resources{
			Memory: "512Mi",
			CPU:    "250m",
		},
	}
}
