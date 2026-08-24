package k8s

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/config"
	"github.com/idlistack/cli/internal/ui"
)

// Deployer manages Kubernetes deployments for IdliStack projects
type Deployer struct {
	config     *config.Config
	plan       *buildplan.Plan
	imageTag   string
	namespace  string
	appName    string // RFC 1123 compliant lowercase name for all K8s resources
	projectDir string
}

// NewDeployer creates a new Deployer instance
func NewDeployer(cfg *config.Config, plan *buildplan.Plan, imageTag string, projectDir string) *Deployer {
	sanitized := sanitizeK8sName(cfg.Project.Name)
	return &Deployer{
		config:     cfg,
		plan:       plan,
		imageTag:   imageTag,
		namespace:  fmt.Sprintf("idlistack-%s", sanitized),
		appName:    sanitized,
		projectDir: projectDir,
	}
}

// sanitizeK8sName converts a project name to a valid RFC 1123 DNS label.
// Must be lowercase alphanumeric, may contain '-' or '.', and must start/end
// with an alphanumeric character.
func sanitizeK8sName(name string) string {
	// Lowercase
	s := strings.ToLower(name)
	// Replace underscores and spaces with hyphens
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	// Remove any characters that aren't alphanumeric, hyphen, or dot
	var result []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '.' {
			result = append(result, c)
		}
	}
	// Trim leading/trailing hyphens and dots
	s = strings.Trim(string(result), "-.")
	if s == "" {
		s = "app"
	}
	return s
}

// Deploy executes the full deployment pipeline
func (d *Deployer) Deploy(ctx context.Context, verbose bool) (string, error) {
	// ─── Deploy Lock ────────────────────────────────────────────────
	lockPath := fmt.Sprintf("/tmp/idlistack-%s.lock", d.appName)
	lockFile, err := acquireLock(lockPath)
	if err != nil {
		return "", fmt.Errorf("deploy lock: another deploy is in progress for this project")
	}
	defer releaseLock(lockFile, lockPath)

	// ─── Clean up previous failed deployments ───────────────────────
	d.cleanupOldDeployment(ctx)

	// ─── Namespace Manager ──────────────────────────────────────────
	ui.Detail("Creating namespace: %s", d.namespace)
	if err := d.ensureNamespace(ctx); err != nil {
		return "", fmt.Errorf("namespace creation failed: %w", err)
	}

	// ─── Ensure Dependency Services (Postgres, Redis, etc.) ─────────
	if err := d.ensureDependencies(ctx); err != nil {
		return "", fmt.Errorf("dependency provisioning failed: %w", err)
	}

	// ─── Pre-deploy jobs ────────────────────────────────────────────
	if d.plan.PreDeployCmd != "" {
		ui.Detail("Running pre-deploy: %s", d.plan.PreDeployCmd)
		if err := d.runPreDeploy(ctx); err != nil {
			return "", fmt.Errorf("pre-deploy failed: %w", err)
		}
	}

	// ─── Apply env secrets ──────────────────────────────────────────
	secretName := fmt.Sprintf("%s-env", d.appName)
	hasSecrets := d.checkSecretsExist(ctx, secretName)

	// ─── Generate and apply manifests ───────────────────────────────
	ui.Detail("Applying Kubernetes manifests")
	if err := d.applyManifests(ctx, hasSecrets, secretName); err != nil {
		return "", fmt.Errorf("manifest apply failed: %w", err)
	}

	// ─── Rollout Status ─────────────────────────────────────────────
	ui.Detail("Waiting for rollout to complete...")
	if err := d.waitForRollout(ctx); err != nil {
		// Show pod logs before rollback so user can see the crash reason
		ui.Warn("Deployment failed, fetching pod logs for debugging...")
		d.showPodLogs(ctx)

		ui.Warn("Attempting rollback...")
		d.rollback(ctx)
		return "", fmt.Errorf("deployment failed (rolled back): %w", err)
	}

	// ─── Get URL ────────────────────────────────────────────────────
	url := d.getServiceURL(ctx)

	return url, nil
}

// ─── Namespace ──────────────────────────────────────────────────────────

func (d *Deployer) ensureNamespace(ctx context.Context) error {
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    managed-by: idlistack
    project: %s
`, d.namespace, d.appName)

	return applyManifest(ctx, manifest)
}

// ─── Manifests ──────────────────────────────────────────────────────────

func (d *Deployer) applyManifests(ctx context.Context, hasSecrets bool, secretName string) error {
	replicas := d.config.Deploy.Replicas
	if replicas == 0 {
		replicas = 1
	}

	port := d.plan.Port
	if d.config.Deploy.Port != 0 {
		port = d.config.Deploy.Port
	}

	// ─── Deployment manifest ────────────────────────────────────────
	envFromSection := ""
	if hasSecrets {
		envFromSection = fmt.Sprintf(`
          envFrom:
          - secretRef:
              name: %s`, secretName)
	}

	// Generate inline env vars from plan (e.g. from docker-compose.yml)
	envSection := ""
	if len(d.plan.Env) > 0 {
		envSection = "\n        env:"
		for k, v := range d.plan.Env {
			envSection += fmt.Sprintf("\n        - name: %s\n          value: \"%s\"", k, v)
		}
	}

	// Use TCP socket probes instead of HTTP — works for ALL apps
	// regardless of whether they have a /health endpoint.
	// startupProbe gives the app up to 150s (30 * 5s) to boot before
	// liveness/readiness probes kick in.
	deploymentManifest := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
    managed-by: idlistack
spec:
  replicas: %d
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
      - name: %s
        image: %s
        imagePullPolicy: Never
        ports:
        - containerPort: %d
        resources:
          requests:
            memory: "%s"
            cpu: "%s"
          limits:
            memory: "%s"
            cpu: "%s"
        startupProbe:
          tcpSocket:
            port: %d
          initialDelaySeconds: 5
          periodSeconds: 5
          failureThreshold: 30
          timeoutSeconds: 3
        livenessProbe:
          tcpSocket:
            port: %d
          initialDelaySeconds: 0
          periodSeconds: 15
          failureThreshold: 3
          timeoutSeconds: 3
        readinessProbe:
          tcpSocket:
            port: %d
          initialDelaySeconds: 0
          periodSeconds: 5
          failureThreshold: 3
          timeoutSeconds: 3%s%s
      securityContext:
        runAsNonRoot: false
`,
		d.appName, d.namespace,
		d.appName,
		replicas,
		d.appName,
		d.appName,
		d.appName,
		d.imageTag,
		port,
		d.plan.Resources.Memory, d.plan.Resources.CPU,
		d.plan.Resources.Memory, d.plan.Resources.CPU,
		port,
		port,
		port,
		envFromSection,
		envSection,
	)

	if err := applyManifest(ctx, deploymentManifest); err != nil {
		return fmt.Errorf("deployment apply failed: %w", err)
	}

	// ─── Service manifest ───────────────────────────────────────────
	serviceManifest := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
    managed-by: idlistack
spec:
  type: NodePort
  selector:
    app: %s
  ports:
  - protocol: TCP
    port: %d
    targetPort: %d
`,
		d.appName, d.namespace,
		d.appName,
		d.appName,
		port, port,
	)

	if err := applyManifest(ctx, serviceManifest); err != nil {
		return fmt.Errorf("service apply failed: %w", err)
	}

	return nil
}

// ─── Pre-deploy ─────────────────────────────────────────────────────────

func (d *Deployer) runPreDeploy(ctx context.Context) error {
	jobManifest := fmt.Sprintf(`apiVersion: batch/v1
kind: Job
metadata:
  name: %s-predeploy
  namespace: %s
spec:
  backoffLimit: 1
  ttlSecondsAfterFinished: 300
  template:
    spec:
      containers:
      - name: predeploy
        image: %s
        imagePullPolicy: Never
        command: ["/bin/sh", "-c", "%s"]
      restartPolicy: Never
`, d.appName, d.namespace, d.imageTag, d.plan.PreDeployCmd)

	// Delete old job if exists
	exec.CommandContext(ctx, "kubectl", "delete", "job",
		fmt.Sprintf("%s-predeploy", d.appName),
		"-n", d.namespace, "--ignore-not-found").Run()

	if err := applyManifest(ctx, jobManifest); err != nil {
		return err
	}

	// Wait for job completion
	waitCmd := exec.CommandContext(ctx, "kubectl", "wait", "--for=condition=complete",
		fmt.Sprintf("job/%s-predeploy", d.appName),
		"-n", d.namespace, "--timeout=300s")
	return waitCmd.Run()
}

// ─── Rollout ────────────────────────────────────────────────────────────

func (d *Deployer) waitForRollout(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "kubectl", "rollout", "status",
		fmt.Sprintf("deployment/%s", d.appName),
		"-n", d.namespace, "--timeout=300s")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (d *Deployer) rollback(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "kubectl", "rollout", "undo",
		fmt.Sprintf("deployment/%s", d.appName),
		"-n", d.namespace)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

// cleanupOldDeployment removes old crashed pods before a fresh deploy
func (d *Deployer) cleanupOldDeployment(ctx context.Context) {
	// Delete the old deployment and pods (but keep the namespace)
	exec.CommandContext(ctx, "kubectl", "delete", "deployment",
		d.appName, "-n", d.namespace, "--ignore-not-found").Run()
	// Brief pause to let Kubernetes process the deletion
	time.Sleep(2 * time.Second)
}

// showPodLogs fetches and prints the last 30 lines of logs from the crashing pod
func (d *Deployer) showPodLogs(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "kubectl", "logs",
		"-n", d.namespace, "-l", fmt.Sprintf("app=%s", d.appName),
		"--tail=30", "--all-containers=true")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		fmt.Println()
		ui.Info("─── Pod Logs (last 30 lines) ───")
		fmt.Println(string(output))
	}
}

// ─── Service URL ────────────────────────────────────────────────────────

func (d *Deployer) getServiceURL(ctx context.Context) string {
	// Try minikube service url
	cmd := exec.CommandContext(ctx, "minikube", "service",
		d.appName, "-n", d.namespace, "--url")
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output))
	}
	return ""
}

// ─── Secrets ────────────────────────────────────────────────────────────

func (d *Deployer) checkSecretsExist(ctx context.Context, secretName string) bool {
	cmd := exec.CommandContext(ctx, "kubectl", "get", "secret", secretName,
		"-n", d.namespace, "--ignore-not-found", "-o", "name")
	output, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(output)) != ""
}

// ─── Deploy Lock ────────────────────────────────────────────────────────

func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("lock already held")
	}

	// Write PID
	fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	return f, nil
}

func releaseLock(f *os.File, path string) {
	if f != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	os.Remove(path)
}

// ─── Helpers ────────────────────────────────────────────────────────────

func applyManifest(ctx context.Context, manifest string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (d *Deployer) extractDbCredentials() (user, pass, dbname string) {
	user = d.appName
	pass = d.appName
	dbname = d.appName

	files := []string{"config.toml", "config.example.toml", ".env", ".env.example", ".env.local"}
	for _, fname := range files {
		fpath := filepath.Join(d.projectDir, fname)
		content, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
				continue
			}
			lower := strings.ToLower(line)
			if strings.Contains(lower, "postgres_user") || (strings.HasPrefix(lower, "user") && strings.Contains(line, "=")) {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					val := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
					if val != "" {
						user = val
					}
				}
			}
			if strings.Contains(lower, "postgres_password") || (strings.HasPrefix(lower, "password") && strings.Contains(line, "=")) {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					val := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
					if val != "" {
						pass = val
					}
				}
			}
			if strings.Contains(lower, "postgres_db") || (strings.HasPrefix(lower, "name") && strings.Contains(line, "=")) || (strings.HasPrefix(lower, "database") && strings.Contains(line, "=")) {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					val := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
					if val != "" && val != "postgres" && val != "db" && val != "localhost" {
						dbname = val
					}
				}
			}
		}
	}
	return user, pass, dbname
}

func (d *Deployer) ensureDependencies(ctx context.Context) error {
	if d.projectDir == "" {
		return nil
	}

	dbUser, dbPass, dbName := d.extractDbCredentials()

	needsPostgres := false
	needsRedis := false
	needsMysql := false

	allDeps := make([]string, 0)
	if len(d.config.Deploy.Dependencies) > 0 {
		allDeps = append(allDeps, d.config.Deploy.Dependencies...)
	}
	if len(d.plan.Dependencies) > 0 {
		allDeps = append(allDeps, d.plan.Dependencies...)
	}

	if len(allDeps) > 0 {
		for _, dep := range allDeps {
			depLower := strings.ToLower(dep)
			if depLower == "postgres" || depLower == "postgresql" || depLower == "db" {
				needsPostgres = true
			}
			if depLower == "redis" {
				needsRedis = true
			}
			if depLower == "mysql" || depLower == "mariadb" {
				needsMysql = true
			}
		}
	} else {
		filesToScan := []string{"docker-compose.yml", "docker-compose.yaml", "config.toml", "config.example.toml", ".env", ".env.example", "idlistack.toml"}
		for _, fname := range filesToScan {
			fpath := filepath.Join(d.projectDir, fname)
			content, err := os.ReadFile(fpath)
			if err != nil {
				continue
			}
			str := strings.ToLower(string(content))
			if strings.Contains(str, "postgres") || strings.Contains(str, "host = \"db\"") || strings.Contains(str, "host=\"db\"") || strings.Contains(str, "db:5432") || strings.Contains(str, "5432") {
				needsPostgres = true
			}
			if strings.Contains(str, "redis") || strings.Contains(str, "host = \"redis\"") || strings.Contains(str, "host=\"redis\"") || strings.Contains(str, "redis:6379") || strings.Contains(str, "6379") {
				needsRedis = true
			}
		}
	}

	if needsPostgres {
		ui.Detail("Provisioning dependency: %s (db: %s, user: %s)", color.CyanString("Postgres (db)"), dbName, dbUser)
		postgresManifest := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: db
  namespace: %[1]s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: db
  template:
    metadata:
      labels:
        app: db
    spec:
      containers:
      - name: postgres
        image: postgres:15-alpine
        imagePullPolicy: IfNotPresent
        env:
        - name: POSTGRES_USER
          value: "%[2]s"
        - name: POSTGRES_PASSWORD
          value: "%[3]s"
        - name: POSTGRES_DB
          value: "%[4]s"
        - name: POSTGRES_HOST_AUTH_METHOD
          value: "trust"
        ports:
        - containerPort: 5432
        readinessProbe:
          exec:
            command: ["pg_isready", "-U", "%[2]s"]
          initialDelaySeconds: 2
          periodSeconds: 2
        volumeMounts:
        - name: db-data
          mountPath: /var/lib/postgresql/data
      volumes:
      - name: db-data
        persistentVolumeClaim:
          claimName: db-pvc
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: db-pvc
  namespace: %[1]s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 5Gi
---
apiVersion: v1
kind: Service
metadata:
  name: db
  namespace: %[1]s
spec:
  selector:
    app: db
  ports:
  - port: 5432
    targetPort: 5432
---
apiVersion: v1
kind: Service
metadata:
  name: postgres-db
  namespace: %[1]s
spec:
  selector:
    app: db
  ports:
  - port: 5432
    targetPort: 5432
`, d.namespace, dbUser, dbPass, dbName)
		_ = applyManifest(ctx, postgresManifest)
		ui.Detail("Waiting for Postgres database to be ready...")
		waitCmd := exec.CommandContext(ctx, "kubectl", "wait", "--for=condition=available", "deployment/db", "-n", d.namespace, "--timeout=300s")
		if err := waitCmd.Run(); err != nil {
			return fmt.Errorf("Postgres failed to become available (timed out after 300s). Check pod logs for ImagePullBackOff or config errors: %w", err)
		}
	}

	if needsMysql {
		ui.Detail("Provisioning dependency: %s (db: %s, user: %s)", color.CyanString("MySQL (db)"), dbName, dbUser)
		mysqlManifest := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: db
  namespace: %[1]s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: db
  template:
    metadata:
      labels:
        app: db
    spec:
      containers:
      - name: mysql
        image: mysql:8.0
        imagePullPolicy: IfNotPresent
        env:
        - name: MYSQL_USER
          value: "%[2]s"
        - name: MYSQL_PASSWORD
          value: "%[3]s"
        - name: MYSQL_DATABASE
          value: "%[4]s"
        - name: MYSQL_ROOT_PASSWORD
          value: "%[3]s"
        ports:
        - containerPort: 3306
        readinessProbe:
          exec:
            command: ["mysqladmin", "ping", "-h", "localhost", "-u", "root", "-p%[3]s"]
          initialDelaySeconds: 10
          periodSeconds: 5
        volumeMounts:
        - name: db-data
          mountPath: /var/lib/mysql
      volumes:
      - name: db-data
        persistentVolumeClaim:
          claimName: db-pvc
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: db-pvc
  namespace: %[1]s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 5Gi
---
apiVersion: v1
kind: Service
metadata:
  name: db
  namespace: %[1]s
spec:
  selector:
    app: db
  ports:
  - port: 3306
    targetPort: 3306
---
apiVersion: v1
kind: Service
metadata:
  name: mysql-db
  namespace: %[1]s
spec:
  selector:
    app: db
  ports:
  - port: 3306
    targetPort: 3306
`, d.namespace, dbUser, dbPass, dbName)
		_ = applyManifest(ctx, mysqlManifest)
		ui.Detail("Waiting for MySQL database to be ready...")
		waitCmd := exec.CommandContext(ctx, "kubectl", "wait", "--for=condition=available", "deployment/db", "-n", d.namespace, "--timeout=300s")
		if err := waitCmd.Run(); err != nil {
			return fmt.Errorf("MySQL failed to become available (timed out after 300s). Check pod logs for ImagePullBackOff or config errors: %w", err)
		}
	}

	if needsRedis {
		ui.Detail("Provisioning dependency: %s", color.CyanString("Redis (redis)"))
		redisManifest := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: %[1]s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
      - name: redis
        image: redis:7-alpine
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 6379
        readinessProbe:
          tcpSocket:
            port: 6379
          initialDelaySeconds: 1
          periodSeconds: 2
        volumeMounts:
        - name: redis-data
          mountPath: /data
      volumes:
      - name: redis-data
        persistentVolumeClaim:
          claimName: redis-pvc
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: redis-pvc
  namespace: %[1]s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
---
apiVersion: v1
kind: Service
metadata:
  name: redis
  namespace: %[1]s
spec:
  selector:
    app: redis
  ports:
  - port: 6379
    targetPort: 6379
`, d.namespace)
		_ = applyManifest(ctx, redisManifest)
		ui.Detail("Waiting for Redis service to be ready...")
		waitCmd := exec.CommandContext(ctx, "kubectl", "wait", "--for=condition=available", "deployment/redis", "-n", d.namespace, "--timeout=300s")
		if err := waitCmd.Run(); err != nil {
			return fmt.Errorf("Redis failed to become available (timed out after 300s). Check pod logs for ImagePullBackOff or config errors: %w", err)
		}
	}
	return nil
}
