# IdliStack CLI — Architecture & Reference

> **Deploy any application with zero configuration.**
> Built by **T4GC** · Written in Go · Deploys to Kubernetes

---

## Overview

IdliStack is a production-grade deployment CLI that automatically **detects** your application's language, framework, runtime version, and dependencies — then **builds** an OCI-compliant container image and **deploys** it to Kubernetes — all with a single command.

```
idlistack up
```

No Dockerfile required. No Kubernetes YAML to write. No CI/CD pipeline to configure.

---

## Technology Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| **CLI Framework** | [Cobra](https://github.com/spf13/cobra) | Command parsing, flags, help text |
| **Configuration** | [TOML](https://toml.io) via BurntSushi/toml | Human-readable project config (`idlistack.toml`) |
| **Detection Engine** | Custom built-in detector | Language, framework, version, and command inference |
| **Image Build** | [Docker Engine](https://docs.docker.com/engine/) | OCI image builds from generated Dockerfiles |
| **Container Runtime** | [Minikube](https://minikube.sigs.k8s.io/) | Local Kubernetes cluster (dev environment) |
| **Orchestration** | [Kubernetes](https://kubernetes.io/) via `kubectl` | Deployment, services, health checks, rollouts |
| **Terminal UI** | [fatih/color](https://github.com/fatih/color) | Colored, structured CLI output |
| **Language** | Go 1.26 | Single binary, cross-platform, zero runtime deps |

---

## Pipeline Architecture

IdliStack executes a **6-step pipeline** every time you run `idlistack up`:

```
┌─────────────────────────────────────────────────────────────────┐
│                     idlistack up                                │
├──────────┬──────────┬──────────┬──────────┬──────────┬──────────┤
│  Step 1  │  Step 2  │  Step 3  │  Step 4  │  Step 5  │  Step 6  │
│ Validate │  Detect  │  Plan    │  Build   │  Load    │  Deploy  │
│ Project  │  App     │  Save    │  OCI     │  Image   │  to K8s  │
└──────────┴──────────┴──────────┴──────────┴──────────┴──────────┘
```

Each step is described in detail below.

---

## Step 1 — Validate Project

**What it does:**
- Reads `idlistack.toml` from the current directory
- Validates that the project has been initialized
- Calculates the source directory size (excluding ignored paths)
- Enforces a dynamic size limit (current size + 500 MB buffer)

**Files read:**
- `idlistack.toml` — Project configuration
- `.idlistackignore` — Custom ignore patterns
- `.gitignore` — Fallback ignore patterns

**Default ignore patterns:**
```
.git, .idlistack, node_modules, __pycache__, .next, dist, build,
.venv, venv, target, vendor, .cargo, .gradle
```

---

## Step 2 — Detect Application

The detection engine is the core intelligence of IdliStack. It identifies **what** your project is and **how** to build and run it — without any user input.

### Detection Layers

```
┌───────────────────────────────────────┐
│  Layer 0: Existing Dockerfile         │  ← Highest priority
│  If Dockerfile exists, use it as-is   │
├───────────────────────────────────────┤
│  Layer 1: Built-in Smart Detector     │  ← Primary detection
│  Scans project files to identify      │
│  language, framework, version, port   │
└───────────────────────────────────────┘
```

### Layer 0 — Dockerfile Detection

If a `Dockerfile` exists in the project root, IdliStack uses it directly without generating anything. It also parses the `EXPOSE` directive to auto-detect the application port.

### Layer 1 — Built-in Smart Detector

The built-in detector walks the source tree and matches **signal files** against a prioritized list of framework signatures.

#### Supported Languages & Frameworks

| Language | Frameworks | Signal Files |
|----------|-----------|-------------|
| **Node.js** | Next.js, Nuxt, Remix, SvelteKit, Astro, Vite, Angular, Gatsby, Ember, Vue, Ghost | `next.config.js`, `nuxt.config.ts`, `vite.config.ts`, `.ghost-cli`, etc. |
| **Python** | Django, Flask, FastAPI, Frappe | `manage.py`, `app.py`, `main.py`, `sites/common_site_config.json` |
| **Go** | Standard Go | `go.mod` |
| **Rust** | Cargo projects | `Cargo.toml` |
| **Ruby** | Rails, Rack | `Gemfile`, `config.ru` |
| **Java** | Maven, Gradle | `pom.xml`, `build.gradle`, `build.gradle.kts` |
| **PHP** | Laravel, Composer | `artisan`, `composer.json` |
| **.NET** | C#, F# | `*.csproj`, `*.fsproj` |
| **Elixir** | Phoenix | `mix.exs` |
| **Deno** | Deno | `deno.json`, `deno.jsonc` |
| **Bun** | Bun | `bun.lockb`, `bunfig.toml` |
| **Static** | HTML/CSS/JS | `index.html` |

#### Dynamic Runtime Version Detection

IdliStack **never hardcodes runtime versions**. It dynamically reads them from your project files, framework metadata, and system runtimes — in priority order:

**Node.js** version sources:
1. `.nvmrc`
2. `.node-version`
3. `package.json` → `engines.node`
4. Framework-specific (e.g., Ghost: `versions/<ver>/package.json` → `engines.node`)
5. System fallback: `node --version`

**Python** version sources:
1. `.python-version`
2. `runtime.txt`
3. `pyproject.toml` → `requires-python`
4. `Pipfile` → `python_version`
5. System fallback: `python3 --version`

**Go** version sources:
1. `go.mod` → `go X.Y`
2. System fallback: `go --version`

**Ruby** version sources:
1. `.ruby-version`
2. `Gemfile` → `ruby 'X.Y.Z'`
3. System fallback: `ruby --version`

**Java** version sources:
1. `.java-version`
2. `pom.xml` → `<java.version>` or `<maven.compiler.source>`
3. `build.gradle` / `build.gradle.kts` → `sourceCompatibility`
4. System fallback: `java --version`

**Rust** version sources:
1. `rust-toolchain.toml` → `channel`
2. `rust-toolchain` (plain file)
3. System fallback: `rustc --version`

**PHP** version sources:
1. `composer.json` → `require.php` or `config.platform.php`
2. System fallback: `php --version`

**Elixir** version sources:
1. `mix.exs` → `elixir: "~> X.Y"`
2. `.tool-versions` (asdf)
3. System fallback: `elixir --version`

**.NET** version sources:
1. `global.json` → `sdk.version`
2. `*.csproj` → `<TargetFramework>netX.Y</TargetFramework>`
3. System fallback: `dotnet --version`

#### Command Inference

After detecting the language and framework, IdliStack infers the install, build, and start commands:

| Framework | Install | Build | Start | Port |
|-----------|---------|-------|-------|------|
| Next.js | `npm install` | `npm run build` | `npm start` | 3000 |
| Ghost | `npm install -g ghost-cli@latest` | — | `ghost run` | 2368 |
| Django | `pip install -r requirements.txt` | — | `python manage.py runserver 0.0.0.0:8000` | 8000 |
| FastAPI | `pip install -r requirements.txt` | — | `uvicorn main:app --host 0.0.0.0 --port 8000` | 8000 |
| Rails | `bundle install` | — | `rails server -b 0.0.0.0 -p 3000` | 3000 |
| Go | — | `go build -o app .` | `./app` | 8080 |
| Maven | — | `mvn clean package -DskipTests` | `java -jar target/*.jar` | 8080 |
| Laravel | `composer install` | — | `php artisan serve --host=0.0.0.0 --port=8000` | 8000 |
| Phoenix | `mix deps.get` | `mix compile` | `mix phx.server` | 4000 |

All inferred values can be **overridden** in `idlistack.toml`.

---

## Step 3 — Generate Build Plan

The detection results are saved as a **build plan** — a JSON file at `.idlistack/buildplan.json`.

```json
{
  "planVersion": "1",
  "provider": "node",
  "runtime": "22",
  "detectedFramework": "ghost",
  "installCmd": "npm install -g ghost-cli@latest",
  "startCmd": "ghost run",
  "port": 2368,
  "healthCheck": { "path": "/health", "interval": 30, "timeout": 5 },
  "resources": { "memory": "512Mi", "cpu": "250m" },
  "detectionConfidence": "medium",
  "detectionSource": "layer2-builtin"
}
```

You can preview the plan without building by running:
```bash
idlistack up --inspect
```

---

## Step 4 — Build OCI Image

IdliStack builds a standard OCI-compliant container image using **Docker Engine**.

### Build Strategy

```
┌──────────────────────────────────────────┐
│  Does the project have a Dockerfile?     │
│                                          │
│   YES → Use it directly                  │
│                                          │
│   NO  → Generate an optimized            │
│          Dockerfile from the build plan  │
└──────────────────────────────────────────┘
```

### Generated Dockerfile

When no Dockerfile exists, IdliStack generates one at `.idlistack/Dockerfile.generated`:

```dockerfile
FROM node:22-alpine        # ← Base image from detected runtime
WORKDIR /app
COPY . .
RUN npm install -g ghost-cli@latest   # ← install command
EXPOSE 2368                           # ← detected port
CMD ["ghost","run"]                   # ← start command
```

### Base Image Resolution

The base image is selected based on the detected provider and runtime version. When no version is detected, Docker's **rolling aliases** are used so images never go stale:

| Provider | Detected Version | Base Image |
|----------|-----------------|------------|
| Node.js | `22` | `node:22-alpine` |
| Node.js | *(none)* | `node:lts-alpine` |
| Python | `3.12` | `python:3.12-slim` |
| Python | *(none)* | `python:3-slim` |
| Go | `1.26` | `golang:1.26-alpine` |
| Go | *(none)* | `golang:1-alpine` |
| Rust | `1.78` | `rust:1.78` |
| Ruby | `3.3` | `ruby:3.3-slim` |
| Java | `21` | `eclipse-temurin:21-jdk-alpine` |
| PHP | `8.2` | `php:8.2-cli` |
| .NET | `8.0` | `mcr.microsoft.com/dotnet/sdk:8.0` |
| Deno | — | `denoland/deno:latest` |
| Bun | — | `oven/bun:latest` |
| Static | — | `nginx:alpine` |

### Build Context Optimization

A `.dockerignore` is auto-generated (if missing) to exclude heavy directories from the Docker build context:

```
.git, .idlistack, node_modules, __pycache__, .next, dist, build,
.venv, venv, target, vendor, .cargo, .gradle, *.sock
```

### Image Tagging

Images are tagged as:
```
idlistack/<project-name>:<unix-timestamp>
```
Example: `idlistack/ghost:1786085692`

---

## Step 5 — Load Image into Minikube

The built image is loaded directly into Minikube's internal Docker daemon:

```bash
minikube image load idlistack/ghost:1786085692
```

This avoids needing a container registry for local development. The Kubernetes manifests use `imagePullPolicy: Never` to reference the locally loaded image.

---

## Step 6 — Deploy to Kubernetes

IdliStack generates and applies Kubernetes manifests for:

### Resources Created

| Resource | Purpose |
|----------|---------|
| **Namespace** | `idlistack-<project>` — isolates the project |
| **Deployment** | Manages pods with the built image |
| **Service** | `NodePort` service for external access |
| **Secret** *(optional)* | Environment variables set via `idlistack env set` |

### Name Sanitization

All Kubernetes resource names are sanitized to comply with **RFC 1123 DNS labels**:
- Lowercased
- Spaces and underscores replaced with hyphens
- Non-alphanumeric characters stripped
- Leading/trailing hyphens removed

Example: `My_App v2` → `my-app-v2`

### Health Checks

Every deployment includes three probes using **TCP socket checks** (works with any application, no `/health` endpoint required):

| Probe | Purpose | Config |
|-------|---------|--------|
| **Startup** | Gives the app time to boot | 5s delay, 5s interval, 30 retries (up to 150s) |
| **Liveness** | Restarts crashed containers | 15s interval, 3 retries |
| **Readiness** | Controls traffic routing | 5s interval, 3 retries |

### Rollout & Rollback

- Waits up to **300 seconds** for the deployment to stabilize
- On failure: fetches the **last 30 lines of pod logs** for debugging
- Automatically **rolls back** to the previous working revision
- Keeps **3 revision history** entries for manual rollbacks

### Deploy Lock

A file-based lock (`/tmp/idlistack-<project>.lock`) prevents concurrent deployments of the same project using `flock`.

---

## Configuration — `idlistack.toml`

```toml
# IdliStack Configuration

[project]
name = "my-app"

[build]
provider = ""              # auto-detected if empty (node, python, go, etc.)
runtime = ""               # auto-detected if empty (22, 3.12, 1.26, etc.)
pre_install_cmd = ""       # system deps (apt-get install ...)
build_cmd = ""             # override detected build command
start_cmd = ""             # override detected start command

[deploy]
port = 0                   # auto-detected if empty
replicas = 1               # number of pod replicas
health_check_path = "/health"

[env]
DATABASE_URL = "postgres://..."
SECRET_KEY = "your-secret"
```

All `[build]` and `[deploy]` values are **optional** — detection fills in sensible defaults. Any value you set **overrides** the detected value.

---

## CLI Commands

### `idlistack init`

Initialize a new project in the current directory.

```bash
idlistack init              # Uses directory name as project name
idlistack init -n my-app    # Custom project name
```

**Creates:**
- `idlistack.toml` — Project configuration
- `.idlistack/` — Internal state directory (gitignored)
- `.idlistack/link.json` — Project metadata

---

### `idlistack up`

Run the full detect → build → deploy pipeline.

```bash
idlistack up                # Full pipeline
idlistack up --inspect      # Preview the build plan only (no build/deploy)
idlistack up -v             # Verbose output with debug info
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--inspect` | Preview the build plan without building or deploying |
| `--detach`, `-d` | Run deployment in the background |
| `--verbose`, `-v` | Enable debug output |

---

### `idlistack down`

Tear down the deployment and delete all Kubernetes resources.

```bash
idlistack down              # Interactive confirmation
idlistack down -f           # Force (skip confirmation)
```

**Deletes:** Namespace, Deployments, Services, Ingresses, Secrets, ConfigMaps.
**Keeps:** Built images in Minikube.

---

### `idlistack status`

Show the current deployment status.

```bash
idlistack status
```

**Displays:**
- Project name and namespace
- Deployment status (Running / Deploying / Down) with replica count
- Service URL (via `minikube service --url`)
- Pod listing with status

---

### `idlistack logs`

Stream live logs from the deployed application.

```bash
idlistack logs              # Stream logs (follow mode)
idlistack logs -f=false     # Print logs without following
idlistack logs -t 50        # Show last 50 lines
```

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--follow`, `-f` | `true` | Follow/stream log output |
| `--tail`, `-t` | `100` | Number of recent log lines to show |

---

### `idlistack env`

Manage environment variables (stored as Kubernetes Secrets).

```bash
idlistack env set KEY=VALUE          # Set a variable
idlistack env set A=1 B=2 C=3       # Set multiple at once
idlistack env list                   # List all variables
idlistack env delete KEY             # Delete a variable
```

Environment variables are injected into pods via `envFrom` → `secretRef`. Run `idlistack up` after changing env vars to apply.

---

### `idlistack version`

Print the CLI version.

```bash
idlistack version
```

---

## Project Structure

```
idlistack/
├── main.go                          # Entry point
├── go.mod                           # Go module (github.com/idlistack/cli)
├── Makefile                         # Build targets
├── cmd/
│   ├── root.go                      # Root command, global flags
│   ├── init.go                      # idlistack init
│   ├── up.go                        # idlistack up (pipeline, build, Dockerfile gen)
│   ├── down.go                      # idlistack down
│   ├── logs.go                      # idlistack logs
│   ├── status.go                    # idlistack status
│   └── env.go                       # idlistack env set/list/delete
├── internal/
│   ├── config/
│   │   └── config.go                # TOML config parser
│   ├── detect/
│   │   └── detect.go                # Detection engine (languages, frameworks, versions)
│   ├── buildplan/
│   │   └── plan.go                  # Build plan struct & defaults
│   ├── k8s/
│   │   └── deployer.go              # Kubernetes deployer (manifests, rollouts, probes)
│   └── ui/
│       └── ui.go                    # Terminal UI (colors, banners, steps)
└── bin/
    └── idlistack                    # Compiled binary
```

---

## Build & Install

```bash
# Build
make build

# Install to ~/.local/bin
make install

# Development build & run
make dev ARGS="up --inspect"

# Run tests
make test

# Clean
make clean
```

### Prerequisites

| Tool | Required For |
|------|-------------|
| **Go 1.22+** | Building the CLI |
| **Docker** | Building OCI images |
| **Minikube** | Local Kubernetes cluster |
| **kubectl** | Kubernetes resource management |

---

## License

Built by **T4GC**.
