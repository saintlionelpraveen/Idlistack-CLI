# IdliStack for Visual Studio Code

<p align="center">
  <strong>Zero-Configuration Application Deployment & Production Console for Kubernetes</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-1.7.0-blue.svg?style=flat-square" alt="Version 1.7.0" />
  <img src="https://img.shields.io/badge/vscode-%3E%3D1.80.0-brightgreen.svg?style=flat-square" alt="VS Code 1.80+" />
  <img src="https://img.shields.io/badge/license-MIT-lightgrey.svg?style=flat-square" alt="License MIT" />
  <img src="https://img.shields.io/badge/kubernetes-K3s%20%7C%20Minikube-orange.svg?style=flat-square" alt="Kubernetes" />
</p>

---

**IdliStack** brings the developer experience of platforms like Railway, Vercel, and Render directly to your local Kubernetes cluster (**K3s**, **Minikube**, **Kind**, or **Docker Desktop**). 

With a single click or command, IdliStack inspects your workspace, detects your programming language, framework, runtime version, and dependencies, generates an optimized multi-stage build plan, builds an OCI image, and deploys it with native Helm charts.

---

## Key Features

### 1. Zero-Config Language & Framework Detection
- **Supported Stacks**: Node.js (Next.js, Express, Nest, Remix), Python (Django, Flask, FastAPI, Frappe), Go, PHP (Laravel, Symfony, WordPress), Rust, Ruby on Rails, Elixir, .NET, Deno, Bun, and static HTML/Nginx.
- **Smart Runtime Resolution**: Automatically detects exact engine versions from `.nvmrc`, `package.json`, `go.mod`, `Pipfile`, `composer.json`, or `.php-version`.

### 2. Automated Companion Dependencies
- Detects database and caching requirements from code imports, configuration files, and SQL schemas.
- Automatically provisions isolated, persistent companion containers:
  - **PostgreSQL 15** with persistent storage
  - **MariaDB 10.6 / MySQL** with persistent storage and automatic schema initialization (`database.sql`)
  - **Redis 7** in-memory cache

### 3. Integrated Production Management Console (`idlistack status`)
A modern, minimalist monochrome web dashboard accessible with one click:
- **Live CPU & Memory Metrics**: Real-time container resource monitoring against configured limits.
- **Interactive Environment Variables Editor**: Full CRUD interface for secrets and environment variables with zero-downtime rolling restarts.
- **In-Browser Container Shell**: Execute debugging commands (`ls -la`, `df -h`, `ps aux`, migrations) directly inside running pods without leaving the browser.
- **1-Click Database SQL Backups**: Instant streaming download of full database SQL dumps.
- **Deployment History & Rollbacks**: Review past Helm revisions and trigger instant 1-click rollbacks.
- **Real-Time Log Streaming**: Tail and filter live container logs.
- **Lifecycle Controls**: Stop, Start, Restart, and dynamically Scale pod replicas.

---

## Available Commands

Access these commands from the Command Palette (`Ctrl+Shift+P` / `Cmd+Shift+P`), the editor context menu, or the persistent status bar item:

| Command | Title | Description |
| :--- | :--- | :--- |
| `idlistack.init` | **IdliStack: Initialize Project** | Generates an `idlistack.toml` configuration template |
| `idlistack.inspect` | **IdliStack: Inspect Stack & Plan** | Previews the detection engine, runtime version, and build plan |
| `idlistack.up` | **IdliStack: Deploy to K3s (Up)** | Detects, builds OCI image, provisions databases, and deploys via Helm |
| `idlistack.status` | **IdliStack: Check Cluster Status** | Displays cluster status and opens the interactive Web Console |
| `idlistack.logs` | **IdliStack: View Logs** | Streams real-time pod logs in the integrated terminal |
| `idlistack.down` | **IdliStack: Destroy (Down)** | Tears down all Kubernetes resources and uninstalls Helm release |
| `idlistack.menu` | **IdliStack: Menu** | Quick-pick menu accessed from the status bar item |

---

## Quick Start Guide

1. Open your project folder in Visual Studio Code.
2. Press `Ctrl+Shift+P` (or `Cmd+Shift+P` on macOS) and type:
   ```
   IdliStack: Deploy to K3s (Up)
   ```
3. IdliStack will automatically detect your project, build the image, and output your live application URL.
4. Click the **IdliStack** status bar item in the bottom-left corner and select **Check Status** to launch your live management console.

---

## Configuration (`idlistack.toml`)

IdliStack works with zero configuration, but you can override any setting by adding an `idlistack.toml` file to your root directory:

```toml
[project]
name = "my-app"

[build]
provider = "node"
runtime = "20"
build_cmd = "npm run build"
start_cmd = "npm start"

[deploy]
port = 3000
replicas = 1
health_check_path = "/health"
dependencies = ["postgres", "redis"]

[env]
NODE_ENV = "production"
```

---

## System Requirements & One-Line Setup

You can install all prerequisites (**Docker**, **K3s**, **Kubectl**, **Helm v3**, and permissions) automatically with our production-ready installer:

```bash
# One-line automated setup (Ubuntu, Debian, Fedora, RHEL, and Windows WSL2):
curl -fsSL https://raw.githubusercontent.com/saintlionelpraveen/Idlistack-CLI/main/install.sh | bash
```

Manual requirements:
- **Visual Studio Code**: Version `1.80.0` or higher
- **Local Kubernetes Cluster**: K3s (recommended), Minikube, Kind, or Docker Desktop Kubernetes
- **Container Engine**: Docker Engine with non-root user access
- **Helm**: Helm v3 CLI installed on system path
- **Kubectl**: Configured to access your local cluster

---

## License

MIT License &bull; Copyright (c) 2026 IdliStack Team
