# IdliStack CLI — Architecture Upgrade Plan (Helm & K3s)

## Overview

The goal of this architectural shift is to enhance IdliStack's deployment capabilities for production readiness by introducing **Helm charts** for Kubernetes orchestration and replacing the development-focused **Minikube** with **K3s**, a lightweight, production-grade Kubernetes distribution.

## Key Architectural Changes

### 1. Replacing Minikube with K3s
Currently, IdliStack relies on Minikube for its local development cluster and uses `minikube image load` and `minikube ip` / `minikube service` to load images and resolve application URLs. 

**Modifications needed:**
- **Image Sideloading (Step 5):** Instead of using `minikube image load <image>`, the CLI will use `docker save <image> -o /tmp/<image>.tar` followed by `sudo k3s ctr images import /tmp/<image>.tar` (or similar containerd mechanisms) to load the built OCI image into the K3s environment.
- **Service URL Resolution:** Instead of relying on `minikube ip`, the CLI will fetch the external IP or Node IP of the K3s cluster via `kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'` (or rely on `localhost` if K3s Traefik/ServiceLB exposes the port directly to the host).
- **Environment Checks:** The CLI should verify K3s is installed (`k3s --version` or `kubectl version`) rather than checking for Minikube.

### 2. Transition from Raw Manifests to Helm Charts
Currently, IdliStack uses string formatting in Go (`internal/k8s/deployer.go`) to generate raw YAML manifests and pipes them to `kubectl apply -f -`.

**Modifications needed:**
- **Helm Chart Generation:** Instead of generating raw strings, the `Deployer` will write a standard Helm chart structure to the `.idlistack/helm` directory:
  ```
  .idlistack/helm/
  ├── Chart.yaml
  ├── values.yaml
  └── templates/
      ├── deployment.yaml
      ├── service.yaml
      ├── job-predeploy.yaml
      ├── secret-env.yaml
      └── dependencies/
          ├── db-deployment.yaml
          ├── db-pvc.yaml
          └── db-service.yaml
  ```
- **Values Substitution:** Dynamic values (like image tag, replicas, ports, database credentials, environment variables) will be written to `values.yaml` rather than being hardcoded into the manifests.
- **Helm Deployment Execution (Step 6):** Instead of running `kubectl apply`, the CLI will execute:
  ```bash
  helm upgrade --install <app-name> .idlistack/helm \
    --namespace <namespace> \
    --create-namespace \
    --wait --timeout 300s
  ```
- **Rollbacks and Status:** 
  - Rollbacks will be performed using `helm rollback <app-name> -n <namespace>`.
  - Deployment status will rely on `helm status` and `kubectl` rollout commands.
- **Teardown (`idlistack down`):** Will run `helm uninstall <app-name> -n <namespace>` instead of deleting individual resources.

## Implementation Steps

1. **Update Dependencies:** Ensure `helm` is added to the system requirements alongside `docker`, `kubectl`, and `k3s`.
2. **Refactor `cmd/up.go`:**
   - Update the "Load Image" step to use K3s container runtime loading instead of Minikube.
3. **Refactor `internal/k8s/deployer.go`:**
   - Add a method to scaffold the Helm chart (create directories and copy template strings to files).
   - Write dynamic configurations into `values.yaml`.
   - Replace the `applyManifest` calls with a single `helm upgrade` command.
   - Refactor `rollback` to use `helm rollback`.
   - Update URL resolution (`getServiceURL`) to fetch the K3s Node IP instead of the Minikube IP.
4. **Update Down Command (`cmd/down.go`):**
   - Refactor the teardown logic to use `helm uninstall`.
5. **Update Status Command (`cmd/status.go`):**
   - Adapt logic to retrieve service NodePort and IP from the K3s cluster.

## Full Pipeline Architecture (End-to-End)

The updated pipeline now executes in 6 distinct steps:

### 1. Validate & Init
- **Technology:** Go (custom logic), TOML parser.
- **Action:** Reads the `idlistack.toml` project configuration and ensures the workspace size is within limits (excluding ignored files via `.idlistackignore` / `.gitignore`).

### 2. Detect Layer
- **Technology:** Built-in Smart Detector (File pattern matching).
- **Action:** Walks the directory to identify the language, framework, runtime version, and port by scanning signal files (like `package.json`, `go.mod`, `Cargo.toml`, etc.).
- **Output:** Resolves all build and start commands dynamically.

### 3. Generate Build Plan
- **Technology:** JSON Serialization.
- **Action:** Saves the detection results into `.idlistack/buildplan.json`. This acts as the source of truth for the rest of the pipeline.

### 4. Build OCI Image
- **Technology:** Docker Engine, BuildKit (Railpack fallback).
- **Action:** Either uses an existing `Dockerfile`, uses Railpack if available, or dynamically generates a multi-stage `Dockerfile` based on the build plan's runtime parameters. Tags the image as `idlistack/<app>:<hash>`.

### 5. Load Image to Container Runtime (K3s)
- **Technology:** Docker CLI, K3s containerd (`k3s ctr`).
- **Action:** Instead of pushing to a slow external registry, it saves the image locally and injects it directly into K3s's containerd socket via:
  ```bash
  docker save idlistack/app:tag -o /tmp/img.tar
  sudo k3s ctr images import /tmp/img.tar
  ```

### 6. Deploy to Kubernetes (Helm)
- **Technology:** Helm CLI, Kubernetes API.
- **Action:** 
  - Scaffolds a native Helm chart dynamically inside `.idlistack/helm/`.
  - Populates `Chart.yaml`, deployments, services, and any dependency databases (Postgres, MySQL, Redis).
  - Uses Helm Hooks to pause rollout until pre-deploy jobs (like DB migrations) succeed.
  - Installs via `helm upgrade --install --wait`.

## Useful K3s Commands

Since we are now using K3s, here are some useful commands you might need when debugging deployments directly:

**Check Cluster Nodes:**
```bash
kubectl get nodes
```
*Shows the status of your local K3s machine.*

**List all running containers in K3s natively (bypassing Docker):**
```bash
sudo k3s crictl ps
```
*Since K3s uses containerd internally, this command shows you the actual running application containers, exactly as Kubernetes sees them.*

**Check K3s System Logs:**
```bash
sudo journalctl -u k3s -f
```
*If K3s is behaving unexpectedly, you can view the systemd journal logs live to diagnose underlying Kubernetes control-plane issues.*
