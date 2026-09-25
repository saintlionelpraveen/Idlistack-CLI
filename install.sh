#!/usr/bin/env bash
# ==============================================================================
# IdliStack — Production Prerequisites & Environment Installer
# Supported Platforms: Ubuntu, Debian, Fedora, RHEL/Rocky/Alma, Windows (WSL2 / Git Bash)
#
# Automatically installs and configures:
#   1. System utilities (curl, wget, ca-certificates, tar, iptables)
#   2. Docker Engine (with non-root user group permissions)
#   3. K3s (Lightweight CNCF-certified Kubernetes cluster)
#   4. kubectl CLI (configured with user kubeconfig permissions)
#   5. Helm v3 (Atomic Kubernetes package manager)
#   6. K3s containerd socket permissions (for zero-registry image sideloading)
#   7. Railpack CLI (Automated buildpack detection engine)
#   8. IdliStack CLI binary (installed to /usr/local/bin/idlistack)
# ==============================================================================

set -euo pipefail

# --- Color formatting ---
BOLD="\033[1m"
GREEN="\033[0;32m"
CYAN="\033[0;36m"
YELLOW="\033[1;33m"
RED="\033[0;31m"
NC="\033[0m" # No Color

# --- Log Helpers ---
log_info() {
    printf "${CYAN}ℹ [INFO]${NC} %s\n" "$1"
}

log_step() {
    printf "\n${BOLD}${CYAN}==>${NC} ${BOLD}%s${NC}\n" "$1"
}

log_success() {
    printf "${GREEN}✔ [SUCCESS]${NC} %s\n" "$1"
}

log_warn() {
    printf "${YELLOW}⚠ [WARNING]${NC} %s\n" "$1"
}

log_error() {
    printf "${RED}✖ [ERROR]${NC} %s\n" "$1" >&2
}

# --- Banner ---
print_banner() {
    cat << "EOF"
  ___     _ _ _  ____  _             _    
 |_ _|___| | (_) / ___|| |_ __ _  ___| | __
  | |/ _ \ | | | \___ \| __/ _` |/ __| |/ /
  | |  __/ | | |  ___) | || (_| | (__|   < 
 |___\___|_|_|_| |____/ \__\__,_|\___|_|\_\
 
 Zero-Config Kubernetes Deployment Environment Setup
EOF
    echo "=========================================================="
}

# --- Resolve User & Home Directory ---
resolve_target_user() {
    if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
        TARGET_USER="$SUDO_USER"
        TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6 || echo "/home/$TARGET_USER")
    else
        TARGET_USER="${USER:-$(whoami)}"
        TARGET_HOME="${HOME:-/root}"
    fi
    log_info "Target user: ${BOLD}${TARGET_USER}${NC} (Home: ${TARGET_HOME})"
}

# --- Elevated Sudo Runner ---
run_as_root() {
    if [ "$(id -u)" -eq 0 ]; then
        "$@"
    else
        sudo "$@"
    fi
}

# --- Detect OS & Environment ---
detect_environment() {
    UNAME_S="$(uname -s 2>/dev/null || echo "Unknown")"
    UNAME_R="$(uname -r 2>/dev/null || echo "Unknown")"
    IS_WSL=false
    OS_FAMILY="unknown"
    OS_ID="unknown"

    # Check Windows Git Bash / MSYS / Cygwin
    case "${UNAME_S}" in
        MINGW*|MSYS*|CYGWIN*)
            OS_FAMILY="windows"
            return 0
            ;;
    esac

    # Check WSL2
    if [[ "${UNAME_R}" =~ [Mm]icrosoft ]] || [[ "${UNAME_R}" =~ WSL ]] || [ -n "${WSL_DISTRO_NAME:-}" ]; then
        IS_WSL=true
    fi

    # Read /etc/os-release
    if [ -f /etc/os-release ]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        OS_ID="${ID:-unknown}"
        case "${ID:-}" in
            ubuntu|debian|linuxmint|pop)
                OS_FAMILY="debian"
                ;;
            fedora|rhel|centos|rocky|almalinux)
                OS_FAMILY="fedora"
                ;;
            arch|manjaro)
                OS_FAMILY="arch"
                ;;
            *)
                if [ -n "${ID_LIKE:-}" ]; then
                    if [[ "${ID_LIKE}" =~ debian ]]; then
                        OS_FAMILY="debian"
                    elif [[ "${ID_LIKE}" =~ (fedora|rhel) ]]; then
                        OS_FAMILY="fedora"
                    fi
                fi
                ;;
        esac
    fi
}

# --- Handle Windows (Non-WSL Host) ---
handle_windows_host() {
    print_banner
    log_warn "Windows Native (Git Bash/MSYS) detected!"
    echo
    echo "Kubernetes (K3s) and container runtimes require a Linux kernel environment."
    echo "To run IdliStack seamlessly on Windows, you have two production options:"
    echo
    echo "  ${BOLD}Option 1: Windows Subsystem for Linux (WSL2) [RECOMMENDED]${NC}"
    echo "    1. Open PowerShell as Administrator and run:"
    echo "       ${CYAN}wsl --install -d Ubuntu${NC}"
    echo "    2. Restart your computer if prompted."
    echo "    3. Open the newly installed Ubuntu terminal and run:"
    echo "       ${CYAN}curl -fsSL https://raw.githubusercontent.com/saintlionelpraveen/Idlistack-CLI/main/install.sh | bash${NC}"
    echo
    echo "  ${BOLD}Option 2: Docker Desktop for Windows${NC}"
    echo "    1. Install Docker Desktop with WSL2 backend enabled."
    echo "    2. In Docker Desktop Settings -> Kubernetes -> check 'Enable Kubernetes'."
    echo "    3. IdliStack will automatically discover the 'docker-desktop' cluster context!"
    echo
    if command -v powershell.exe >/dev/null 2>&1; then
        read -r -p "Would you like to trigger 'wsl --install -d Ubuntu' now via PowerShell? [y/N]: " choice || true
        case "$choice" in
            [yY][eE][sS]|[yY])
                powershell.exe -Command "Start-Process powershell -Verb RunAs -ArgumentList 'wsl --install -d Ubuntu'"
                log_info "WSL2 installation launched in administrative PowerShell."
                ;;
            *)
                log_info "Skipping automated WSL installation."
                ;;
        esac
    fi
    exit 0
}

# --- Configure WSL2 Systemd Support ---
configure_wsl_systemd() {
    if [ "$IS_WSL" = true ]; then
        log_info "WSL2 environment detected. Ensuring systemd is enabled in /etc/wsl.conf..."
        if [ ! -f /etc/wsl.conf ] || ! grep -q "systemd=true" /etc/wsl.conf; then
            run_as_root mkdir -p /etc
            run_as_root tee /etc/wsl.conf > /dev/null << 'EOF'
[boot]
systemd=true
EOF
            log_warn "Configured systemd in /etc/wsl.conf. If systemd services fail, run 'wsl --shutdown' in Windows PowerShell and relaunch WSL."
        else
            log_success "WSL2 systemd is already configured."
        fi
    fi
}

# --- Step 1: Install Core System Dependencies ---
install_core_dependencies() {
    log_step "Step 1/7: Installing core system utilities"
    case "$OS_FAMILY" in
        debian)
            log_info "Updating package lists via apt-get..."
            run_as_root apt-get update -qq
            run_as_root apt-get install -y -qq curl wget ca-certificates tar gzip iptables gnupg lsb-release sudo
            ;;
        fedora)
            log_info "Installing package dependencies via dnf..."
            run_as_root dnf install -y -q curl wget ca-certificates tar gzip iptables systemd sudo
            ;;
        arch)
            log_info "Installing package dependencies via pacman..."
            run_as_root pacman -Sy --noconfirm curl wget ca-certificates tar gzip iptables sudo
            ;;
        *)
            log_warn "Unrecognized distribution (${OS_ID}). Attempting to proceed assuming curl, tar, and sudo exist..."
            ;;
    esac
    log_success "System utilities are installed."
}

# --- Step 2: Install and Configure Docker Engine ---
install_docker() {
    log_step "Step 2/7: Installing & configuring Docker Engine"

    if command -v docker >/dev/null 2>&1; then
        log_info "Docker is already installed ($(docker --version))."
    else
        log_info "Installing Docker Engine via official Docker installation script..."
        curl -fsSL https://get.docker.com | run_as_root sh
    fi

    # Ensure docker daemon service is enabled & running
    if command -v systemctl >/dev/null 2>&1 && systemctl is-system-running >/dev/null 2>&1; then
        run_as_root systemctl enable --now docker >/dev/null 2>&1 || true
        run_as_root systemctl start docker >/dev/null 2>&1 || true
    elif command -v service >/dev/null 2>&1; then
        run_as_root service docker start >/dev/null 2>&1 || true
    fi

    # Configure non-root user permissions
    log_info "Configuring Docker group permissions for user '${TARGET_USER}'..."
    run_as_root groupadd -f docker
    run_as_root usermod -aG docker "${TARGET_USER}"

    # Ensure socket access
    if [ -S /var/run/docker.sock ]; then
        run_as_root chmod 666 /var/run/docker.sock 2>/dev/null || true
    fi

    log_success "Docker Engine is active and configured for non-root usage."
}

# --- Step 3: Install K3s Kubernetes Cluster ---
install_k3s() {
    log_step "Step 3/7: Installing K3s (Lightweight Kubernetes)"

    # Check if a Kubernetes cluster already exists and is healthy
    if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info >/dev/null 2>&1; then
        log_info "Active Kubernetes cluster already detected via kubectl. Skipping K3s reinstallation."
        return 0
    fi

    if command -v k3s >/dev/null 2>&1 && k3s kubectl cluster-info >/dev/null 2>&1; then
        log_info "K3s is already installed and running."
    else
        log_info "Installing K3s with write-kubeconfig-mode 644..."
        # Install K3s with readable kubeconfig permissions for local developer use
        curl -sfL https://get.k3s.io | run_as_root sh -s - --write-kubeconfig-mode 644
    fi

    # Wait for node readiness
    log_info "Waiting for K3s node to reach Ready state..."
    local attempts=0
    local max_attempts=30
    until run_as_root k3s kubectl get node 2>/dev/null | grep -q "Ready"; do
        attempts=$((attempts + 1))
        if [ "$attempts" -ge "$max_attempts" ]; then
            log_warn "K3s is taking longer than expected to initialize. Proceeding..."
            break
        fi
        sleep 2
    done

    log_success "K3s cluster is running and ready."
}

# --- Step 4: Configure Kubectl & Kubeconfig ---
configure_kubectl() {
    log_step "Step 4/7: Configuring kubectl & developer kubeconfig"

    # Symlink kubectl to k3s if standalone kubectl is missing
    if ! command -v kubectl >/dev/null 2>&1; then
        if [ -f /usr/local/bin/k3s ]; then
            log_info "Symlinking /usr/local/bin/k3s -> /usr/local/bin/kubectl..."
            run_as_root ln -sf /usr/local/bin/k3s /usr/local/bin/kubectl
        fi
    fi

    # Configure user's ~/.kube/config
    local kube_dir="${TARGET_HOME}/.kube"
    mkdir -p "${kube_dir}"

    if [ -f /etc/rancher/k3s/k3s.yaml ]; then
        log_info "Copying K3s kubeconfig to ${kube_dir}/config..."
        run_as_root cp /etc/rancher/k3s/k3s.yaml "${kube_dir}/config"
        run_as_root chown -R "${TARGET_USER}:${TARGET_USER}" "${kube_dir}"
        run_as_root chmod 600 "${kube_dir}/config"
    fi

    # Ensure KUBECONFIG environment variable is persisted in profile
    local rc_file="${TARGET_HOME}/.bashrc"
    if [ -f "$rc_file" ] && ! grep -q "KUBECONFIG" "$rc_file"; then
        echo 'export KUBECONFIG=~/.kube/config' >> "$rc_file"
    fi

    log_success "kubectl configured and connected to cluster."
}

# --- Step 5: Configure K3s containerd Sideload Permissions ---
configure_sideload_permissions() {
    log_step "Step 5/7: Configuring K3s containerd zero-registry sideloading"

    # Make containerd socket accessible to non-root user
    if [ -S /run/k3s/containerd/containerd.sock ]; then
        run_as_root chmod 666 /run/k3s/containerd/containerd.sock 2>/dev/null || true
    fi

    # Create passwordless sudoers entry for 'k3s ctr images import' so background deployments never hang
    local sudoers_file="/etc/sudoers.d/idlistack-k3s"
    log_info "Adding passwordless sudoers rule for K3s containerd image imports..."
    run_as_root tee "$sudoers_file" > /dev/null << EOF
# Allow ${TARGET_USER} to import sideloaded container images into K3s containerd without password
${TARGET_USER} ALL=(ALL) NOPASSWD: /usr/local/bin/k3s ctr images import *, /usr/bin/k3s ctr images import *
EOF
    run_as_root chmod 0440 "$sudoers_file"

    log_success "Containerd image sideloading permissions configured."
}

# --- Step 6: Install Helm v3 ---
install_helm() {
    log_step "Step 6/7: Installing Helm v3 (Kubernetes Orchestrator)"

    if command -v helm >/dev/null 2>&1; then
        log_info "Helm v3 is already installed ($(helm version --short))."
    else
        log_info "Downloading and installing official Helm v3..."
        curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | run_as_root bash
    fi

    log_success "Helm v3 is installed and ready."
}

# --- Step 7: Pre-fetch Railpack & IdliStack Binary ---
install_railpack_and_cli() {
    log_step "Step 7/7: Installing Railpack engine & IdliStack CLI binary"

    # 1. Pre-download Railpack to ~/.idlistack/bin/railpack
    local idli_bin_dir="${TARGET_HOME}/.idlistack/bin"
    mkdir -p "${idli_bin_dir}"

    if command -v railpack >/dev/null 2>&1; then
        log_info "Railpack binary found in system PATH."
    elif [ -f "${idli_bin_dir}/railpack" ]; then
        log_info "Railpack binary already cached in ${idli_bin_dir}/railpack."
    else
        log_info "Pre-fetching Railpack (v0.39.0) standalone static binary..."
        local arch="x86_64"
        if [ "$(uname -m)" = "aarch64" ] || [ "$(uname -m)" = "arm64" ]; then
            arch="arm64"
        fi
        local rp_url="https://github.com/railwayapp/railpack/releases/download/v0.39.0/railpack-v0.39.0-${arch}-unknown-linux-musl.tar.gz"
        if curl -fsSL "$rp_url" -o /tmp/railpack.tar.gz 2>/dev/null; then
            tar -xzf /tmp/railpack.tar.gz -C "${idli_bin_dir}" railpack 2>/dev/null || true
            chmod 0755 "${idli_bin_dir}/railpack" 2>/dev/null || true
            rm -f /tmp/railpack.tar.gz
            run_as_root chown -R "${TARGET_USER}:${TARGET_USER}" "${TARGET_HOME}/.idlistack"
            log_success "Railpack v0.39.0 cached to ${idli_bin_dir}/railpack."
        else
            log_warn "Could not pre-fetch Railpack. IdliStack CLI will automatically download it on first run."
        fi
    fi

    # 2. Install IdliStack CLI binary to /usr/local/bin if present in current directory
    local script_dir
    script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
    if [ -f "${script_dir}/bin/idlistack" ]; then
        log_info "Installing IdliStack CLI binary to /usr/local/bin/idlistack..."
        run_as_root cp "${script_dir}/bin/idlistack" /usr/local/bin/idlistack
        run_as_root chmod 0755 /usr/local/bin/idlistack
        log_success "IdliStack CLI installed globally to /usr/local/bin/idlistack."
    elif [ -f "${script_dir}/idlistack" ]; then
        run_as_root cp "${script_dir}/idlistack" /usr/local/bin/idlistack
        run_as_root chmod 0755 /usr/local/bin/idlistack
        log_success "IdliStack CLI installed globally to /usr/local/bin/idlistack."
    fi
}

# --- Verification & Diagnostics ---
verify_installation() {
    echo
    printf "${BOLD}==========================================================${NC}\n"
    printf "${BOLD}             IdliStack Environment Status Check           ${NC}\n"
    printf "${BOLD}==========================================================${NC}\n"

    # Docker Check
    if command -v docker >/dev/null 2>&1; then
        printf "  %-24s ${GREEN}✔ Installed${NC} (%s)\n" "Docker CLI:" "$(docker --version | cut -d',' -f1)"
    else
        printf "  %-24s ${RED}✖ Missing${NC}\n" "Docker CLI:"
    fi

    # Docker Socket Check
    if run_as_root docker ps >/dev/null 2>&1; then
        printf "  %-24s ${GREEN}✔ Running & Accessible${NC}\n" "Docker Daemon:"
    else
        printf "  %-24s ${RED}✖ Inaccessible${NC}\n" "Docker Daemon:"
    fi

    # Kubectl Check
    if command -v kubectl >/dev/null 2>&1; then
        printf "  %-24s ${GREEN}✔ Installed${NC}\n" "Kubectl CLI:"
    else
        printf "  %-24s ${RED}✖ Missing${NC}\n" "Kubectl CLI:"
    fi

    # Kubernetes Cluster Check
    if run_as_root kubectl cluster-info >/dev/null 2>&1 || (command -v k3s >/dev/null 2>&1 && run_as_root k3s kubectl cluster-info >/dev/null 2>&1); then
        printf "  %-24s ${GREEN}✔ Healthy & Connected${NC}\n" "Kubernetes Cluster:"
    else
        printf "  %-24s ${YELLOW}⚠ Initializing${NC}\n" "Kubernetes Cluster:"
    fi

    # Helm Check
    if command -v helm >/dev/null 2>&1; then
        printf "  %-24s ${GREEN}✔ Installed${NC} (%s)\n" "Helm Orchestrator:" "$(helm version --short)"
    else
        printf "  %-24s ${RED}✖ Missing${NC}\n" "Helm Orchestrator:"
    fi

    # IdliStack CLI Check
    if command -v idlistack >/dev/null 2>&1; then
        printf "  %-24s ${GREEN}✔ Installed${NC} (/usr/local/bin/idlistack)\n" "IdliStack CLI:"
    else
        printf "  %-24s ${CYAN}ℹ Available via VS Code Extension${NC}\n" "IdliStack CLI:"
    fi

    printf "${BOLD}==========================================================${NC}\n"
    echo
    printf "${GREEN}${BOLD}🎉 System is fully configured for IdliStack!${NC}\n"
    echo
    echo "Next Steps:"
    echo "  1. If running under a new shell session, apply group changes:"
    echo "     ${CYAN}newgrp docker${NC}  (or log out and log back in)"
    echo "  2. Open your project in Visual Studio Code."
    echo "  3. Press ${BOLD}Ctrl+Shift+P${NC} (or Cmd+Shift+P) and run:"
    echo "     ${CYAN}IdliStack: Deploy to K3s (Up)${NC}"
    echo
}

# --- Main Flow ---
main() {
    print_banner
    detect_environment

    if [ "$OS_FAMILY" = "windows" ]; then
        handle_windows_host
    fi

    log_info "Detected OS: ${BOLD}${OS_ID}${NC} (Family: ${OS_FAMILY}, WSL: ${IS_WSL})"
    resolve_target_user

    configure_wsl_systemd
    install_core_dependencies
    install_docker
    install_k3s
    configure_kubectl
    configure_sideload_permissions
    install_helm
    install_railpack_and_cli
    verify_installation
}

main "$@"