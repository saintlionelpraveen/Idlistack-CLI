package buildplan

// Plan represents the IdliStack build plan — the central artifact of detection.
// This is what gets cached, versioned, and inspected via `idlistack up --inspect`.
type Plan struct {
	PlanVersion         string            `json:"planVersion"`
	Provider            string            `json:"provider"`
	Runtime             string            `json:"runtime"`
	DetectedFramework   string            `json:"detectedFramework"`
	InstallCmd          string            `json:"installCmd,omitempty"`
	PreInstallCmd       string            `json:"preInstallCmd,omitempty"`
	BuildCmd            string            `json:"buildCmd,omitempty"`
	StartCmd            string            `json:"startCmd"`
	Port                int               `json:"port"`
	DockerfilePath      string            `json:"dockerfilePath,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`
	HealthCheck         HealthCheck       `json:"healthCheck"`
	Resources           Resources         `json:"resources"`
	PreDeployCmd        string            `json:"preDeployCmd,omitempty"`
	StaticDir           string            `json:"staticDir,omitempty"`
	DetectionConfidence string            `json:"detectionConfidence"`
	DetectionSource     string            `json:"detectionSource"`
	User                string            `json:"user,omitempty"`
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
