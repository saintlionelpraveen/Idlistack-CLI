# IdliStack CLI — Production Architecture & Technical Reference

> **Deploy any application with zero configuration.**
> Designed for local environments and production edge clusters. Powered by Go, Docker, Helm, and K3s.

---

## 1. Executive Summary & Design Philosophy

IdliStack is a production-grade deployment orchestrator that abstracts the complexities of containerization and Kubernetes. It is designed to act as an invisible, intelligent pipeline that automatically **detects** an application's stack, **compiles** an optimized OCI-compliant image, and **deploys** it to a lightweight K3s cluster using native Helm charts.

### Core Tenets
- **Zero Configuration:** Developers should not need to write `Dockerfiles`, Kubernetes YAMLs, or CI/CD pipelines to achieve a production-ready local deployment.
- **Dynamic Adaptability:** Hardcoded versions are anti-patterns. IdliStack dynamically infers runtimes from source code (e.g., `package.json`, `go.mod`, `.ruby-version`).
- **Production-Grade Tooling:** Moving away from development-only tools (like Minikube and raw manifests), IdliStack utilizes **K3s** (a CNCF-certified Kubernetes distribution) and **Helm** (the industry-standard package manager) to ensure parity with real-world production environments.

---

## 2. Technology Stack

| Component | Technology | Role & Purpose |
|-----------|-----------|---------|
| **CLI Framework** | [Cobra](https://github.com/spf13/cobra) (Go) | Handles command parsing, subcommands (`up`, `down`, `status`, `env`), flags, and contextual help text. |
| **Configuration** | TOML | Parses the `idlistack.toml` file for project-specific overrides. |
| **Detection Engine** | Native Go | Custom multi-layered engine to infer language, framework, runtime version, and start commands. |
| **Image Builder** | Docker Engine & BuildKit | Compiles the inferred architecture into OCI-compliant images using multi-stage Dockerfiles. |
| **Container Runtime** | K3s (containerd) | A highly efficient, single-binary Kubernetes distribution replacing Minikube for native execution. |
| **Orchestrator** | Helm | Scaffolds and manages Kubernetes resources atomically (upgrades, rollbacks, hooks). |

---

## 3. The Deployment Pipeline Lifecycle

The core of IdliStack is the `idlistack up` command, which executes a strict 6-stage lifecycle. 

### 3.1. Workspace Validation & Initialization
Before any heavy lifting occurs, the CLI validates the integrity of the workspace.
- **Config Parsing:** Reads `idlistack.toml` to identify manual overrides (e.g., explicitly defined ports or build commands).
- **Size Boundary Checks:** Recursively calculates the size of the source directory. It respects `.idlistackignore` and `.gitignore` to prevent uploading massive artifacts (like `node_modules` or `.git`) to the Docker build context. It enforces a dynamic size limit (Current Size + 500MB buffer) to prevent system memory exhaustion.

### 3.2. Dynamic Detection Engine
The detection engine is the "brain" of IdliStack. It scans the workspace to determine *what* the application is and *how* it should be run.

- **Layer 0 (Escape Hatch):** If a `Dockerfile` is present in the root directory, IdliStack bypasses native detection and uses the provided Dockerfile. It parses the `EXPOSE` directive to infer network ports.
- **Layer 1 (Native Providers):** The built-in detector scans for **Signal Files**.
  - **Node.js:** Looks for `package.json`, `next.config.js`, `.ghost-cli`. Infers Node version from engines or `.nvmrc`.
  - **Python:** Looks for `requirements.txt`, `pyproject.toml`, `manage.py`.
  - **Go:** Parses `go.mod` for the Go version.
  - **Rust, Ruby, PHP, Java:** Matches specific lockfiles and manifests.
- **Command Inference:** Based on the framework detected (e.g., Next.js vs Ghost CMS), it injects the exact `Install`, `Build`, and `Start` commands required.

### 3.3. Build Plan Generation
The outputs of the Detection layer are serialized into a strict JSON schema and saved to `.idlistack/buildplan.json`. 
- **Purpose:** This file acts as the single source of truth. It allows developers to run `idlistack up --inspect` to preview the build strategy without triggering a deployment, ensuring transparency.

### 3.4. OCI Image Compilation
IdliStack transforms the Build Plan into a runnable container image.
- **Base Image Resolution:** Selects an optimized, minimal base image based on the detected runtime (e.g., `node:22-bookworm`, `golang:1.26-alpine`). If no version is specified, it defaults to rolling aliases (like `lts` or `latest`) to prevent staleness.
- **Multi-Stage Dockerfile Generation:** Scaffolds `.idlistack/Dockerfile.generated`.
  - **Stage 1 (Builder):** Copies source code, runs pre-install scripts, package managers, and build binaries.
  - **Stage 2 (Runtime):** Strips away build tools, sets user permissions, copies compiled artifacts from the builder, injects ENV variables, and defines the `CMD`.
- **Execution:** Triggers `docker build` using BuildKit caching optimizations. The image is tagged dynamically: `idlistack/<project-name>:<unix-timestamp>`.

### 3.5. Runtime Injection (Sideloading)
To avoid the latency and configuration overhead of pushing images to an external Docker registry (like Docker Hub or AWS ECR), IdliStack injects the image directly into K3s.
- **Process:** It exports the built image into a tarball (`docker save ... -o /tmp/img.tar`).
- **Import:** It utilizes the K3s internal containerd runtime (`sudo k3s ctr images import`) to sideload the image. Kubernetes manifests use `imagePullPolicy: Never` to ensure it boots instantly from the local cache.

### 3.6. Helm & Kubernetes Orchestration
The final stage abstracts Kubernetes YAML templating and applies it natively.
- **Scaffolding:** IdliStack creates a valid Helm chart structure inside `.idlistack/helm/`, generating `Chart.yaml` and writing dynamic deployment templates (Deployments, Services, PVCs) into the `templates/` directory.
- **Atomic Upgrades:** Executes `helm upgrade --install <app> .idlistack/helm --namespace <ns> --create-namespace --wait --timeout 300s`. 
- **Rollback Protection:** If the rollout fails (e.g., the application crashes on boot), the `--wait` flag catches the failure. IdliStack immediately fetches the last 30 lines of container logs for debugging and issues a `helm rollback` to revert the cluster to the last stable state.

---

## 4. Subsystems & Key Workflows

### 4.1. Dependency Injection (Databases & Caches)
IdliStack dynamically provisions backing services (MySQL, Postgres, Redis) if it detects their usage in environment variables or configuration files.
- **Stateful Deployment:** Generates standard `Deployment` manifests with `PersistentVolumeClaims` (PVCs) attached to ensure database persistence across rollouts.
- **Lifecycle Integration:** These dependencies are deployed alongside the application via Helm. The application pods use robust TCP Socket `startupProbes` and `livenessProbes` to gracefully handle the delay while the databases initialize.

### 4.2. Traffic Routing & Networking
- **Port Management:** The detection engine identifies the application's binding port (e.g., `3000` for Next.js, `8000` for Django).
- **Service Exposure:** Creates a K8s `Service` of type `NodePort`. 
- **Endpoint Resolution:** Dynamically fetches the K3s Internal IP via `kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'` and combines it with the NodePort to provide the user with a clickable, routable HTTP URL.

### 4.3. Pre-Deploy Migrations (Helm Hooks)
If the application requires pre-deployment tasks (like database schema migrations), IdliStack generates a K8s `Job`.
- **Helm Hook Annotations:** The Job is annotated with `"helm.sh/hook": pre-install,pre-upgrade`.
- **Execution:** Helm halts the rollout of the main application containers until the Job completes successfully, guaranteeing data consistency.

### 4.4. Environment Variable & Secret Management
Managed via the `idlistack env` command suite.
- Variables are saved as Kubernetes `Opaque Secrets`.
- The Helm deployment injects these securely into the application pods using the `envFrom: secretRef` specification.

---

## 5. Directory Structure & Code Organization

```text
idlistack/
├── main.go                          # CLI Entry point
├── Makefile                         # Build targets (build, install, clean, test)
├── cmd/                             # Cobra Command Implementations
│   ├── root.go                      # Global flags and root context
│   ├── init.go                      # Workspace initialization
│   ├── up.go                        # The primary 6-stage pipeline orchestrator
│   ├── down.go                      # Helm uninstall and namespace teardown
│   ├── logs.go                      # Streaming log aggregator
│   ├── status.go                    # Cluster health and URL resolution
│   └── env.go                       # Secret management
├── internal/                        # Core Engine Logic
│   ├── config/                      # TOML parsing schemas
│   ├── detect/                      # Multi-layer framework detection engine
│   ├── buildplan/                   # Build plan data structures
│   ├── k8s/                         # Kubernetes logic
│   │   └── deployer.go              # Helm scaffolder, upgrader, and rollback manager
│   └── ui/                          # Terminal UI components (colorization, spinners)
```

---

## 6. Production K3s Management & Debugging

The transition to K3s provides native, production-equivalent debugging pathways. If IdliStack encounters issues, you can inspect the state directly via standard K8s interfaces.

### Core Debugging Commands
- **View Cluster State:**
  ```bash
  kubectl get all -n idlistack-<app-name>
  ```
- **Inspect Helm Releases:**
  ```bash
  helm list -A
  helm history <app-name> -n idlistack-<app-name>
  ```
- **Bypass Docker (Direct Containerd Access):**
  K3s runs its own containerd socket. To view running containers natively:
  ```bash
  sudo k3s crictl ps
  ```
- **System Logs:**
  To diagnose node-level or K3s control-plane failures:
  ```bash
  sudo journalctl -u k3s -f
  ```

---

## 7. Future Extensibility

The architecture is designed to be highly modular. Future iterations can easily adopt:
1. **Cloud Deployments:** Changing the `.kube/config` context allows `idlistack up` to target remote production clusters (AWS EKS, DigitalOcean) natively via Helm.
2. **Ingress Controllers:** The K3s `NodePort` approach can be seamlessly upgraded to emit `Ingress` manifests pointing to K3s's native Traefik router for custom domain binding.
3. **Provider Expansion:** Adding support for new languages merely requires adding a new file under `internal/detect/` to match signal files.
