#!/bin/bash
# Install NVIDIA GPU base stack + common agent frameworks on Ubuntu 24.04 (Noble).
# Target: x86_64 with NVIDIA GPU (CUDA 12.x network repo).
#
# Usage:
#   sudo /opt/agent/agent-install-nvidia.sh
#   sudo FORCE=0 /opt/agent/agent-install-nvidia.sh
#
# Optional overrides (examples):
#   NVIDIA_DRIVER_PACKAGE=nvidia-driver-550-open
#   CUDA_META_PACKAGE=cuda-toolkit-12-8
#   INSTALL_CUDA_TOOLKIT=1   (default 1)
#   INSTALL_CUDNN=1          (default 1)
#   INSTALL_NCCL=1           (default 1)
#   INSTALL_CONTAINER_TOOLKIT=1 (default 1)
#   INSTALL_PYTORCH_CUDA=1   (default 1; pip torch with CUDA in venv)
#   INSTALL_VLLM=0           (default 0; large, enable if needed)
#   INSTALL_HERMES=1  INSTALL_OPENCLAW=1  INSTALL_NEMOCLAW=0
#   INSTALL_HOST_OPENCLAW_WITH_NEMOCLAW=0  (set 1 to also install host OpenClaw when NemoClaw=1)
#   NEMOCLAW_INSTALL_URL=https://www.nvidia.com/nemoclaw.sh  (redirects to NVIDIA/NemoClaw installer)
#   OPENCLAW_INSTALL_URL=https://openclaw.ai/install.sh
#   HERMES_INSTALL_FLAGS="--skip-setup --non-interactive --skip-browser"
#   HERMES_INSTALL_AS_USER=
#
# Agent (OS) layer — public install paths (Jun 2026):
#   Hermes    — hermes-agent.nousresearch.com/install.sh
#   OpenClaw  — openclaw.ai/install.sh (--no-onboard); npm global also supported
#   NemoClaw  — curl|bash nvidia.com/nemoclaw.sh; needs Docker + GPU; bundles OpenClaw in OpenShell
#
# Rerunnable: apt every run; stamped custom steps every run (FORCE=1 default). FORCE=0 to skip stamps.
# Requires: network, root, writable rootfs, NVIDIA GPU + driver load after reboot if new driver.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
readonly LOG_TAG="agent-install-nvidia"
readonly STAMP_DIR="/var/lib/agent-install-nvidia/done"
readonly LOG_FILE="/var/log/agent-install-nvidia.log"
readonly AGENT_VENV="/opt/agent/venv-nvidia"

readonly CUDA_DIST="${CUDA_DIST:-ubuntu2404}"
readonly CUDA_ARCH="${CUDA_ARCH:-x86_64}"
readonly CUDA_KEYRING_DEB="${CUDA_KEYRING_DEB:-cuda-keyring_1.1-1_all.deb}"
readonly CUDA_KEYRING_URL="${CUDA_KEYRING_URL:-https://developer.download.nvidia.com/compute/cuda/repos/${CUDA_DIST}/${CUDA_ARCH}/${CUDA_KEYRING_DEB}}"

readonly NVIDIA_DRIVER_PACKAGE="${NVIDIA_DRIVER_PACKAGE:-nvidia-driver-550-open}"
readonly CUDA_META_PACKAGE="${CUDA_META_PACKAGE:-cuda-toolkit-12-8}"

INSTALL_CUDA_TOOLKIT="${INSTALL_CUDA_TOOLKIT:-1}"
INSTALL_CUDNN="${INSTALL_CUDNN:-1}"
INSTALL_NCCL="${INSTALL_NCCL:-1}"
INSTALL_CONTAINER_TOOLKIT="${INSTALL_CONTAINER_TOOLKIT:-1}"
INSTALL_PYTORCH_CUDA="${INSTALL_PYTORCH_CUDA:-1}"
INSTALL_VLLM="${INSTALL_VLLM:-0}"
INSTALL_HERMES="${INSTALL_HERMES:-1}"
INSTALL_OPENCLAW="${INSTALL_OPENCLAW:-1}"
INSTALL_NEMOCLAW="${INSTALL_NEMOCLAW:-0}"
INSTALL_HOST_OPENCLAW_WITH_NEMOCLAW="${INSTALL_HOST_OPENCLAW_WITH_NEMOCLAW:-0}"

readonly NEMOCLAW_INSTALL_URL="${NEMOCLAW_INSTALL_URL:-https://www.nvidia.com/nemoclaw.sh}"
readonly OPENCLAW_INSTALL_URL="${OPENCLAW_INSTALL_URL:-https://openclaw.ai/install.sh}"
readonly HERMES_INSTALL_URL="${HERMES_INSTALL_URL:-https://hermes-agent.nousresearch.com/install.sh}"
HERMES_INSTALL_FLAGS="${HERMES_INSTALL_FLAGS:---skip-setup --non-interactive --skip-browser}"
HERMES_INSTALL_AS_USER="${HERMES_INSTALL_AS_USER:-}"

# Installed after cuda-keyring + apt update (adjust names if apt reports alternatives).
PACKAGES=(
	ca-certificates
	curl
	wget
	git
	xz-utils
	gnupg
	apt-transport-https
	python3
	python3-pip
	python3-venv
	"${NVIDIA_DRIVER_PACKAGE}"
)

log() {
	echo "[${LOG_TAG}] $(date -u +%Y-%m-%dT%H:%M:%SZ) $*" | tee -a "${LOG_FILE}"
}

require_root() {
	if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
		echo "${SCRIPT_NAME}: run as root (e.g. sudo ${SCRIPT_NAME})" >&2
		exit 1
	fi
}

run_once_step() {
	local id="$1"
	shift
	local stamp="${STAMP_DIR}/${id}"

	if [[ -f "${stamp}" && "${FORCE:-1}" != "1" ]]; then
		log "Skip step '${id}' (already done; default FORCE=1 re-runs; set FORCE=0 to skip)"
		return 0
	fi

	log "Step '${id}' start"
	bash -c "$@"
	touch "${stamp}"
	log "Step '${id}' ok"
}

run_once_step_cuda_keyring() {
	run_once_step "nvidia-cuda-keyring" "
		set -euo pipefail
		tmp=\$(mktemp)
		curl -fsSL '${CUDA_KEYRING_URL}' -o \"\${tmp}\"
		dpkg -i \"\${tmp}\"
		rm -f \"\${tmp}\"
	"
}

run_once_step_container_toolkit_repo() {
	run_once_step "nvidia-container-toolkit-repo" '
		set -euo pipefail
		install -d -m 0755 /usr/share/keyrings
		curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey \
			| gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
		curl -fsSL https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list \
			| sed "s#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g" \
			> /etc/apt/sources.list.d/nvidia-container-toolkit.list
	'
}

run_once_step_hermes() {
	local hermes_pipe="curl -fsSL '${HERMES_INSTALL_URL}' | bash -s -- ${HERMES_INSTALL_FLAGS}"
	run_once_step "hermes-agent-v2" "
		set -euo pipefail
		export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a UV_NO_CONFIG=1
		if [[ -n '${HERMES_INSTALL_AS_USER}' ]]; then
			sudo -u $(printf '%q' '${HERMES_INSTALL_AS_USER}') -H env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a UV_NO_CONFIG=1 bash -c $(printf '%q' "${hermes_pipe}") </dev/null
		else
			bash -c $(printf '%q' "${hermes_pipe}") </dev/null
		fi
	"
}

run_once_step_openclaw() {
	run_once_step "openclaw-agent" \
		"curl -fsSL '${OPENCLAW_INSTALL_URL}' | bash -s -- --no-onboard"
}

run_once_step_nemoclaw() {
	run_once_step "nemoclaw-stack" "
		set -euo pipefail
		export NEMOCLAW_ACCEPT_THIRD_PARTY_SOFTWARE=1
		curl -fsSL '${NEMOCLAW_INSTALL_URL}' | bash -s -- --yes-i-accept-third-party-software
	"
}

run_once_step_agent_python_venv() {
	run_once_step "agent-python-venv-nvidia" "
		set -euo pipefail
		python3 -m venv '${AGENT_VENV}'
		'${AGENT_VENV}/bin/pip' install -U pip wheel
		'${AGENT_VENV}/bin/pip' install \\
			autogen-agentchat \\
			crewai \\
			langgraph \\
			openai \\
			openai-agents
	"
}

run_once_step_pytorch_cuda() {
	run_once_step "pytorch-cuda-venv" "
		set -euo pipefail
		'${AGENT_VENV}/bin/pip' install torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cu124
	"
}

run_once_step_vllm() {
	run_once_step "vllm-pip" "
		set -euo pipefail
		'${AGENT_VENV}/bin/pip' install vllm
	"
}

append_optional_cuda_packages() {
	local extra=()

	if [[ "${INSTALL_CUDA_TOOLKIT}" == "1" ]]; then
		extra+=("${CUDA_META_PACKAGE}")
	fi
	if [[ "${INSTALL_CUDNN}" == "1" ]]; then
		extra+=(libcudnn9-cuda-12 libcudnn9-dev-cuda-12)
	fi
	if [[ "${INSTALL_NCCL}" == "1" ]]; then
		extra+=(libnccl2 libnccl-dev)
	fi
	if [[ "${INSTALL_CONTAINER_TOOLKIT}" == "1" ]]; then
		extra+=(nvidia-container-toolkit)
	fi
	if [[ "${INSTALL_NEMOCLAW}" == "1" ]]; then
		extra+=(docker.io)
	fi

	PACKAGES+=("${extra[@]}")
}

install_apt_packages() {
	if [[ ${#PACKAGES[@]} -eq 0 ]]; then
		log "No PACKAGES configured; skipping apt install"
		return 0
	fi

	export DEBIAN_FRONTEND=noninteractive
	export DEBCONF_NONINTERACTIVE_SEEN=true

	log "apt-get update"
	apt-get update -y

	log "apt-get install (${#PACKAGES[@]} packages)"
	apt-get install -y --no-install-recommends "${PACKAGES[@]}"
}

configure_container_toolkit() {
	if [[ "${INSTALL_CONTAINER_TOOLKIT}" != "1" ]]; then
		return 0
	fi
	if command -v nvidia-ctk >/dev/null 2>&1; then
		log "nvidia-ctk runtime configure --runtime=containerd (if using containerd)"
		nvidia-ctk runtime configure --runtime=containerd || log "containerd configure skipped (not installed)"
	fi
}

enable_docker_for_nemoclaw() {
	if [[ "${INSTALL_NEMOCLAW}" != "1" ]]; then
		return 0
	fi
	if command -v systemctl >/dev/null 2>&1; then
		systemctl enable --now docker || log "WARN: could not enable docker.service"
	fi
}

should_install_host_openclaw() {
	if [[ "${INSTALL_OPENCLAW}" != "1" ]]; then
		return 1
	fi
	if [[ "${INSTALL_NEMOCLAW}" == "1" && "${INSTALL_HOST_OPENCLAW_WITH_NEMOCLAW}" != "1" ]]; then
		return 1
	fi
	return 0
}

install_agent_os_layers() {
	if [[ "${INSTALL_HERMES}" == "1" ]]; then
		run_once_step_hermes
	fi
	if should_install_host_openclaw; then
		run_once_step_openclaw
	elif [[ "${INSTALL_OPENCLAW}" == "1" && "${INSTALL_NEMOCLAW}" == "1" ]]; then
		log "Skip host OpenClaw (NemoClaw ships OpenClaw in OpenShell); set INSTALL_HOST_OPENCLAW_WITH_NEMOCLAW=1 to install both"
	fi
	if [[ "${INSTALL_NEMOCLAW}" == "1" ]]; then
		run_once_step_nemoclaw
	fi
}

warn_if_no_gpu() {
	if command -v nvidia-smi >/dev/null 2>&1; then
		if ! nvidia-smi >/dev/null 2>&1; then
			log "WARN: nvidia-smi failed — reboot may be required after driver install"
		fi
	else
		log "WARN: nvidia-smi not in PATH yet; reboot after driver install if this is first setup"
	fi
}

main() {
	require_root
	mkdir -p "$(dirname "${LOG_FILE}")" "${STAMP_DIR}" /opt/agent
	: >> "${LOG_FILE}"

	log "=== ${SCRIPT_NAME} start (FORCE=${FORCE:-1}) ==="

	run_once_step_cuda_keyring
	append_optional_cuda_packages

	if [[ "${INSTALL_CONTAINER_TOOLKIT}" == "1" ]]; then
		run_once_step_container_toolkit_repo
	fi

	install_apt_packages
	configure_container_toolkit
	enable_docker_for_nemoclaw

	install_agent_os_layers
	run_once_step_agent_python_venv

	if [[ "${INSTALL_PYTORCH_CUDA}" == "1" ]]; then
		run_once_step_pytorch_cuda
	fi
	if [[ "${INSTALL_VLLM}" == "1" ]]; then
		run_once_step_vllm
	fi

	warn_if_no_gpu

	log "=== ${SCRIPT_NAME} complete ==="
	log "Activate venv: ${AGENT_VENV}/bin/activate"
	if [[ "${INSTALL_HERMES}" == "1" ]]; then
		log "Hermes: configure manually — hermes setup (optional: hermes gateway install)"
	fi
	if should_install_host_openclaw || [[ "${INSTALL_OPENCLAW}" == "1" ]]; then
		log "OpenClaw: run 'openclaw onboard' when ready (skipped on host if NemoClaw-only)"
	fi
	if [[ "${INSTALL_NEMOCLAW}" == "1" ]]; then
		log "NemoClaw: finish onboarding — docs.nvidia.com/nemoclaw (set inference provider env for non-interactive)"
	else
		log "NemoClaw: INSTALL_NEMOCLAW=1 sudo ${SCRIPT_NAME} (needs Docker, GPU, third-party accept flags)"
	fi
}

main "$@"
