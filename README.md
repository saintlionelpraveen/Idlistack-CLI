# IdliStack

Deploy applications with zero configuration. Powered by Railpack + BuildKit + Minikube.

## Quick Start

```bash
# Initialize a project
idlistack init

# Deploy (detect → build → deploy)
idlistack up

# Check status
idlistack status

# Stream logs
idlistack logs

# Tear down
idlistack down
```

## Commands

| Command | Description |
|---|---|
| `idlistack init` | Initialize a new project |
| `idlistack up` | Full pipeline: detect → build → deploy |
| `idlistack up --inspect` | Preview build plan without deploying |
| `idlistack status` | Show deployment status |
| `idlistack logs` | Stream live logs |
| `idlistack env set K=V` | Set environment variables |
| `idlistack env list` | List environment variables |
| `idlistack down` | Tear down deployment |

## Supported Languages & Frameworks

Node.js, Python, Go, Rust, Ruby, Java, PHP, Elixir, .NET, Deno, Bun, and static sites.

## Prerequisites

- Docker
- Minikube
- Buildpack
- kubectl
