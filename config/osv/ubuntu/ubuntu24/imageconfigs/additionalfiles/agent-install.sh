#!/bin/bash
# dash/sh do not support "set -o pipefail"; re-exec with bash if needed.
case "${BASH_VERSION:-}" in
'') exec /bin/bash "$0" "$@" ;;
esac

# Install Intel agent stack + common agent frameworks on Ubuntu 22.04 / 24.04.
# Target: x86_64 with Intel GPU/NPU (set INTEL_APT_ARCH=arm64 on aarch64 hosts).
#
# Intel apt suites are ubuntu22 / ubuntu24 (not jammy/noble). Override:
#   INTEL_UBUNTU_SUITE=ubuntu22|ubuntu24
#
# Usage (must run under bash — not "sh agent-install.sh"):
#   sudo /opt/agent/agent-install.sh
#   sudo bash /opt/agent/agent-install.sh
#   sudo FORCE=0 /opt/agent/agent-install.sh   # skip stamped steps if already done
#
# Agent (OS) layer (diagram) — public install paths researched Jun 2026:
#   Hermes     — install.sh with --skip-setup --non-interactive (user runs hermes setup later)
#   OpenClaw   — curl|bash https://openclaw.ai/install.sh (--no-onboard for scripts)
#   SuperClaw  — Windows desktop + WSL/Docker; Linux edge = superclaw-ctl binary only
#                (intel/intel-ai-builder superclaw/superclaw-ctl/USER-GUIDE.md)
#
# Optional env (defaults):
#   INSTALL_HERMES=1  INSTALL_OPENCLAW=1  INSTALL_SUPERCLAW_CTL=0
#   INTEL_PACKAGE_POLICY=release  # release | latest | pinned
#   OPENVINO_RELEASE=2026.2.0      # target; installs when Intel publishes openvino-2026.2.0
#   OPENVINO_RELEASE_FALLBACK=1    # 1 = newest openvino-* meta if target not in apt yet
#   OPENVINO_REPO_TRACK=2025       # Intel apt path …/openvino/${OPENVINO_REPO_TRACK}
#   INSTALL_OPEN_MODEL_ZOO=1       # git clone open_model_zoo (tag OPEN_MODEL_ZOO_TAG)
#   INSTALL_INTEL_GPU_REPO=1       # add repositories.intel.com/gpu for up-to-date Level Zero (L0) runtime
#   INTEL_GPU_REPO_COMPONENT=unified  # Intel GPU repo component (unified | client)
#   INSTALL_PYTORCH_XPU=1          # pip install torch+XPU backend in the agent venv
#   PYTORCH_XPU_INDEX_URL=https://download.pytorch.org/whl/xpu
#   OPENCLAW_INSTALL_URL=https://openclaw.ai/install.sh
#   SUPERCLAW_CTL_URL=…/superclaw-ctl-v1.0.0-linux-x86-64.tar.gz
#   SUPERCLAW_CTL_PREFIX=/opt/superclaw
#   HERMES_INSTALL_FLAGS="--skip-setup --non-interactive --skip-browser"  # skips Playwright only; Hermes may still install Node
#   HERMES_INSTALL_AS_USER=         # optional; empty = install as script user (root → /usr/local/…)
#   AGENT_INSTALL_PROXY_MODE=auto   # auto | on | off — see configure_network_proxy
#   AGENT_INSTALL_HTTP_PROXY=http://proxy-dmz.intel.com:911
#   AGENT_INSTALL_HTTPS_PROXY=http://proxy-dmz.intel.com:912
#
# Rerunnable: apt-get update/install every run; stamped custom steps every run (FORCE=1 default).
# Set FORCE=0 to skip completed stamp steps. Requires: network, root, writable apt/dpkg.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
readonly SCRIPT_REV="2026-06-30-intel-l0-gpu-pytorch-xpu-v15"
readonly LOG_TAG="agent-install"
readonly STAMP_DIR="/var/lib/agent-install/done"
readonly LOG_FILE="/var/log/agent-install.log"
readonly AGENT_VENV="/opt/agent/venv"

readonly OPENCLAW_INSTALL_URL="${OPENCLAW_INSTALL_URL:-https://openclaw.ai/install.sh}"
readonly HERMES_INSTALL_URL="${HERMES_INSTALL_URL:-https://hermes-agent.nousresearch.com/install.sh}"
# Install binaries/deps only; skip setup wizard and gateway prompts (see Hermes install.sh).
HERMES_INSTALL_FLAGS="${HERMES_INSTALL_FLAGS:---skip-setup --non-interactive --skip-browser}"
HERMES_INSTALL_AS_USER="${HERMES_INSTALL_AS_USER:-}"
readonly SUPERCLAW_CTL_URL="${SUPERCLAW_CTL_URL:-https://github.com/intel/intel-ai-builder/raw/main/superclaw/superclaw-ctl/binary_build/superclaw-ctl-v1.0.0-linux-x86-64.tar.gz}"
readonly SUPERCLAW_CTL_PREFIX="${SUPERCLAW_CTL_PREFIX:-/opt/superclaw}"
readonly INTEL_APT_ARCH="${INTEL_APT_ARCH:-amd64}"
readonly OPENVINO_REPO_TRACK="${OPENVINO_REPO_TRACK:-2025}"
readonly OPENVINO_RELEASE="${OPENVINO_RELEASE:-2026.2.0}"
OPENVINO_RELEASE_FALLBACK="${OPENVINO_RELEASE_FALLBACK:-1}"
readonly OPEN_MODEL_ZOO_DIR="${OPEN_MODEL_ZOO_DIR:-/opt/intel/open_model_zoo}"
readonly OPEN_MODEL_ZOO_GIT_URL="${OPEN_MODEL_ZOO_GIT_URL:-https://github.com/openvinotoolkit/open_model_zoo.git}"
OPEN_MODEL_ZOO_TAG="${OPEN_MODEL_ZOO_TAG:-}"

AGENT_INSTALL_PROXY_MODE="${AGENT_INSTALL_PROXY_MODE:-auto}"
AGENT_INSTALL_HTTP_PROXY="${AGENT_INSTALL_HTTP_PROXY:-http://proxy-dmz.intel.com:911}"
AGENT_INSTALL_HTTPS_PROXY="${AGENT_INSTALL_HTTPS_PROXY:-http://proxy-dmz.intel.com:912}"
AGENT_INSTALL_NO_PROXY="${AGENT_INSTALL_NO_PROXY:-}"
AGENT_INSTALL_PROXY_PROBE_URL="${AGENT_INSTALL_PROXY_PROBE_URL:-https://apt.repos.intel.com/intel-gpg-keys/GPG-PUB-KEY-INTEL-SW-PRODUCTS.PUB}"

INSTALL_HERMES="${INSTALL_HERMES:-1}"
INSTALL_OPENCLAW="${INSTALL_OPENCLAW:-1}"
INSTALL_SUPERCLAW_CTL="${INSTALL_SUPERCLAW_CTL:-0}"
INSTALL_OPEN_MODEL_ZOO="${INSTALL_OPEN_MODEL_ZOO:-1}"
INTEL_PACKAGE_POLICY="${INTEL_PACKAGE_POLICY:-release}"

# Up-to-date Intel GPU Level Zero (L0) runtime: Ubuntu's stock libze-intel-gpu1 lags badly.
INSTALL_INTEL_GPU_REPO="${INSTALL_INTEL_GPU_REPO:-1}"
readonly INTEL_GPU_REPO_COMPONENT="${INTEL_GPU_REPO_COMPONENT:-unified}"

# PyTorch with Intel GPU (XPU) backend, installed into the agent venv from the XPU wheel index.
INSTALL_PYTORCH_XPU="${INSTALL_PYTORCH_XPU:-1}"
readonly PYTORCH_XPU_INDEX_URL="${PYTORCH_XPU_INDEX_URL:-https://download.pytorch.org/whl/xpu}"

# Used only when INTEL_PACKAGE_POLICY=pinned (exact deb names; OpenVINO uses release-style meta names).
INTEL_PINNED_PACKAGES=(
	openvino-2026.2.0
	intel-oneapi-runtime-compilers_2025.3.3-30
	intel-oneapi-runtime-compilers-common_2025.3.3-30
	intel-oneapi-runtime-opencl_2025.3.3-30
	intel-dlstreamer_2025.2.0
)

# Newest matching deb in Intel apt after each apt-get update (default policy).
INTEL_LATEST_APT_PATTERNS=(
	'^intel-oneapi-runtime-compilers_'
	'^intel-oneapi-runtime-compilers-common_'
	'^intel-oneapi-runtime-opencl_'
	'^intel-dlstreamer_'
)

# Debian package names (every run). Intel OpenVINO/oneAPI/DL Streamer added in resolve_packages_for_install.
PACKAGES=(
	ca-certificates
	curl
	wget
	git
	xz-utils
	gnupg
	apt-transport-https
	cmake
	g++
	gcc
	make
	pkgconf
	python3
	python3-pip
	python3-venv

	libze1
	libze-intel-gpu1
	intel-level-zero-npu
	intel-driver-compiler-npu

	xpu-smi

	podman
)

log() {
	echo "[${LOG_TAG}] $(date -u +%Y-%m-%dT%H:%M:%SZ) $*" | tee -a "${LOG_FILE}" >&2
}

require_root() {
	if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
		echo "${SCRIPT_NAME}: run as root (e.g. sudo ${SCRIPT_NAME})" >&2
		exit 1
	fi
}

proxy_env_already_set() {
	[[ -n "${http_proxy:-${HTTP_PROXY:-}}" || -n "${https_proxy:-${HTTPS_PROXY:-}}" ]]
}

sync_proxy_env_from_existing() {
	local h="${http_proxy:-${HTTP_PROXY:-}}"
	local s="${https_proxy:-${HTTPS_PROXY:-}}"
	local n="${no_proxy:-${NO_PROXY:-}}"

	if [[ -n "${h}" ]]; then
		export http_proxy="${h}" HTTP_PROXY="${h}"
	fi
	if [[ -n "${s}" ]]; then
		export https_proxy="${s}" HTTPS_PROXY="${s}"
	fi
	if [[ -n "${n}" ]]; then
		export no_proxy="${n}" NO_PROXY="${n}"
	fi
}

apply_agent_install_default_proxy() {
	export http_proxy="${AGENT_INSTALL_HTTP_PROXY}"
	export https_proxy="${AGENT_INSTALL_HTTPS_PROXY}"
	export HTTP_PROXY="${http_proxy}"
	export HTTPS_PROXY="${https_proxy}"
	if [[ -n "${AGENT_INSTALL_NO_PROXY}" ]]; then
		export no_proxy="${AGENT_INSTALL_NO_PROXY}"
		export NO_PROXY="${no_proxy}"
	fi
}

network_https_reachable() {
	local url="$1"
	local direct="$2"

	if ! command -v curl >/dev/null 2>&1; then
		return 0
	fi
	if [[ "${direct}" == "1" ]]; then
		curl -fsSL --connect-timeout 8 --max-time 20 --noproxy '*' -o /dev/null "${url}" 2>/dev/null
	else
		curl -fsSL --connect-timeout 8 --max-time 20 -o /dev/null "${url}" 2>/dev/null
	fi
}

# auto: keep user/sudo env; else direct probe; else Intel DMZ defaults. off: never set. on: always set if unset.
configure_network_proxy() {
	local mode="${AGENT_INSTALL_PROXY_MODE}"
	local probe="${AGENT_INSTALL_PROXY_PROBE_URL}"

	sync_proxy_env_from_existing
	if proxy_env_already_set; then
		log "Network proxy: using existing env (http_proxy=${http_proxy:-<unset>}, https_proxy=${https_proxy:-<unset>})"
		return 0
	fi

	case "${mode}" in
	off)
		log "Network proxy: AGENT_INSTALL_PROXY_MODE=off (not setting http_proxy/https_proxy)"
		return 0
		;;
	on)
		apply_agent_install_default_proxy
		log "Network proxy: mode=on — set http_proxy=${http_proxy}, https_proxy=${https_proxy}"
		return 0
		;;
	auto)
		;;
	*)
		log "WARN: unknown AGENT_INSTALL_PROXY_MODE=${mode}; treating as auto"
		;;
	esac

	if network_https_reachable "${probe}" "1"; then
		log "Network proxy: direct HTTPS OK; not setting proxy (AGENT_INSTALL_PROXY_MODE=auto)"
		return 0
	fi

	if ! command -v curl >/dev/null 2>&1; then
		log "Network proxy: curl unavailable for probe; not auto-setting proxy"
		return 0
	fi

	log "Network proxy: direct probe failed for ${probe}; trying default Intel DMZ proxy"
	apply_agent_install_default_proxy
	if network_https_reachable "${probe}" "0"; then
		log "Network proxy: using defaults (http_proxy=${http_proxy}, https_proxy=${https_proxy})"
		return 0
	fi

	log "WARN: HTTPS still failing via default proxy; unsetting auto-proxy (set http_proxy/https_proxy manually)"
	unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY no_proxy NO_PROXY
}

detect_intel_ubuntu_suite() {
	if [[ -n "${INTEL_UBUNTU_SUITE:-}" ]]; then
		echo "${INTEL_UBUNTU_SUITE}"
		return 0
	fi

	local ver_id ver_codename
	# shellcheck disable=SC1091
	ver_id="$(source /etc/os-release && echo "${VERSION_ID:-}")"
	# shellcheck disable=SC1091
	ver_codename="$(source /etc/os-release && echo "${VERSION_CODENAME:-}")"

	case "${ver_id}" in
	24.04 | 24.*) echo "ubuntu24" ;;
	22.04 | 22.*) echo "ubuntu22" ;;
	*)
		case "${ver_codename}" in
		noble) echo "ubuntu24" ;;
		jammy) echo "ubuntu22" ;;
		*)
			return 1
			;;
		esac
		;;
	esac
}

# repositories.intel.com/gpu uses Ubuntu codenames (noble/jammy), not the ubuntu24/ubuntu22 suites.
detect_intel_gpu_codename() {
	case "$(detect_intel_ubuntu_suite)" in
	ubuntu24) echo "noble" ;;
	ubuntu22) echo "jammy" ;;
	*) return 1 ;;
	esac
}

intel_apt_sources_ok() {
	local suite
	suite="$(detect_intel_ubuntu_suite)" || return 1
	[[ -f /etc/apt/sources.list.d/intel-openvino.list ]] || return 1
	grep -qF "openvino/${OPENVINO_REPO_TRACK} ${suite} main" /etc/apt/sources.list.d/intel-openvino.list || return 1
	[[ -f /etc/apt/sources.list.d/intel-dlstreamer.list ]] || return 1
	grep -qF "dlstreamer/${suite} ${suite} main" /etc/apt/sources.list.d/intel-dlstreamer.list || return 1
	[[ -f /etc/apt/sources.list.d/intel-oneapi.list ]] || return 1
	if [[ "${INSTALL_INTEL_GPU_REPO}" == "1" ]]; then
		local gpu_codename
		gpu_codename="$(detect_intel_gpu_codename)" || return 0
		[[ -f /etc/apt/sources.list.d/intel-gpu.list ]] || return 1
		grep -qF "gpu/ubuntu ${gpu_codename} ${INTEL_GPU_REPO_COMPONENT}" /etc/apt/sources.list.d/intel-gpu.list || return 1
	fi
	return 0
}

# ICT image templates may ship package-repositories.list (no signed-by) for the same
# Intel URLs as intel-*.list (signed-by=…). Apt rejects conflicting Signed-By values.
remove_ict_duplicate_intel_apt_lines() {
	local f="/etc/apt/sources.list.d/package-repositories.list"

	if [[ ! -f "${f}" ]]; then
		return 0
	fi
	if ! grep -q 'apt.repos.intel.com' "${f}"; then
		return 0
	fi
	if [[ ! -f /etc/apt/sources.list.d/intel-openvino.list ]]; then
		return 0
	fi

	log "Removing duplicate Intel entries from ${f} (intel-*.list provides signed-by sources)"
	local tmp
	tmp="$(mktemp)"
	grep -v 'apt.repos.intel.com' "${f}" >"${tmp}" || true
	install -m 0644 "${tmp}" "${f}"
	rm -f "${tmp}"
}

configure_intel_apt_repos_files() {
	local suite="$1"
	local dls_base="$2"
	local gpu_codename="$3"

	remove_ict_duplicate_intel_apt_lines

	bash -c "
		set -euo pipefail
		install -d -m 0755 /usr/share/keyrings

		curl -fsSL https://apt.repos.intel.com/intel-gpg-keys/GPG-PUB-KEY-INTEL-SW-PRODUCTS.PUB \
			| gpg --batch --yes --dearmor -o /usr/share/keyrings/intel-sw-products.gpg

		cat >/etc/apt/sources.list.d/intel-openvino.list <<EOF
deb [arch=${INTEL_APT_ARCH} signed-by=/usr/share/keyrings/intel-sw-products.gpg] https://apt.repos.intel.com/openvino/${OPENVINO_REPO_TRACK} ${suite} main
EOF

		cat >/etc/apt/sources.list.d/intel-oneapi.list <<EOF
deb [arch=${INTEL_APT_ARCH} signed-by=/usr/share/keyrings/intel-sw-products.gpg] https://apt.repos.intel.com/oneapi all main
EOF

		curl -fsSL https://apt.repos.intel.com/edgeai/dlstreamer/GPG-PUB-KEY-INTEL-DLS.gpg \
			| gpg --batch --yes --dearmor -o /usr/share/keyrings/intel-dls.gpg

		cat >/etc/apt/sources.list.d/intel-dlstreamer.list <<EOF
deb [arch=${INTEL_APT_ARCH} signed-by=/usr/share/keyrings/intel-dls.gpg] ${dls_base} ${suite} main
EOF

		if [[ -n '${gpu_codename}' ]]; then
			curl -fsSL https://repositories.intel.com/gpu/intel-graphics.key \
				| gpg --batch --yes --dearmor -o /usr/share/keyrings/intel-graphics.gpg
			cat >/etc/apt/sources.list.d/intel-gpu.list <<EOF
deb [arch=${INTEL_APT_ARCH} signed-by=/usr/share/keyrings/intel-graphics.gpg] https://repositories.intel.com/gpu/ubuntu ${gpu_codename} ${INTEL_GPU_REPO_COMPONENT}
EOF
		else
			rm -f /etc/apt/sources.list.d/intel-gpu.list
		fi
	"
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

run_once_step_intel_apt_repos() {
	local suite dls_base stamp="${STAMP_DIR}/intel-apt-repos-v2"

	suite="$(detect_intel_ubuntu_suite)" || {
		log "ERROR: unsupported Ubuntu for Intel repos (need 22.04 or 24.04, or set INTEL_UBUNTU_SUITE=ubuntu22|ubuntu24)"
		exit 1
	}
	dls_base="https://apt.repos.intel.com/edgeai/dlstreamer/${suite}"
	log "Intel apt suite: ${suite} (arch=${INTEL_APT_ARCH})"

	local gpu_codename=""
	if [[ "${INSTALL_INTEL_GPU_REPO}" == "1" ]]; then
		gpu_codename="$(detect_intel_gpu_codename || true)"
		if [[ -n "${gpu_codename}" ]]; then
			log "Intel GPU (Level Zero) repo: repositories.intel.com/gpu/ubuntu ${gpu_codename} ${INTEL_GPU_REPO_COMPONENT}"
		else
			log "WARN: could not map suite to Intel GPU repo codename; skipping L0 runtime repo"
		fi
	fi

	if [[ -f "${STAMP_DIR}/intel-apt-repos" ]]; then
		log "Removing obsolete stamp intel-apt-repos (wrong noble/jammy suite lists)"
		rm -f "${STAMP_DIR}/intel-apt-repos"
	fi

	if [[ -f "${stamp}" && "${FORCE:-1}" != "1" ]] && intel_apt_sources_ok; then
		log "Skip step 'intel-apt-repos-v2' (repos OK; set FORCE=0 to skip)"
		return 0
	fi

	if ! intel_apt_sources_ok; then
		log "Intel apt sources missing or wrong — writing intel-openvino/intel-oneapi/intel-dlstreamer lists"
	fi

	log "Step 'intel-apt-repos-v2' start"
	configure_intel_apt_repos_files "${suite}" "${dls_base}" "${gpu_codename}"
	touch "${stamp}"
	log "Step 'intel-apt-repos-v2' ok"
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

run_once_step_superclaw_ctl() {
	run_once_step "superclaw-ctl-edge" "
		set -euo pipefail
		tmp=\$(mktemp -d)
		trap 'rm -rf \"\${tmp}\"' EXIT
		curl -fsSL '${SUPERCLAW_CTL_URL}' -o \"\${tmp}/superclaw-ctl.tgz\"
		tar -xzf \"\${tmp}/superclaw-ctl.tgz\" -C \"\${tmp}\"
		install -d -m 0755 '${SUPERCLAW_CTL_PREFIX}/bin'
		install -m 0755 \"\${tmp}/superclaw-ctl\" '${SUPERCLAW_CTL_PREFIX}/bin/superclaw-ctl'
		ln -sf '${SUPERCLAW_CTL_PREFIX}/bin/superclaw-ctl' /usr/local/bin/superclaw-ctl
	"
}

run_once_step_open_model_zoo() {
	if [[ "${INSTALL_OPEN_MODEL_ZOO}" != "1" ]]; then
		return 0
	fi

	local omz_tag
	omz_tag="$(resolve_open_model_zoo_tag)" || return 1

	run_once_step "open-model-zoo-${omz_tag}" "
		set -euo pipefail
		dir='${OPEN_MODEL_ZOO_DIR}'
		tag='${omz_tag}'
		url='${OPEN_MODEL_ZOO_GIT_URL}'
		parent=\$(dirname \"\${dir}\")
		install -d -m 0755 \"\${parent}\"
		if [[ -d \"\${dir}/.git\" ]]; then
			git -C \"\${dir}\" fetch --tags origin
			git -C \"\${dir}\" checkout \"\${tag}\"
		else
			git clone --depth 1 --branch \"\${tag}\" \"\${url}\" \"\${dir}\"
		fi
		install -d -m 0755 /etc/profile.d
		printf '%s\\n' \"export OMZ_ROOT=\${dir}\" \"export OPEN_MODEL_ZOO=\${dir}\" \"export OMZ_GIT_TAG=\${tag}\" > /etc/profile.d/open-model-zoo.sh
		chmod 0644 /etc/profile.d/open-model-zoo.sh
		if [[ -f \"\${dir}/requirements.txt\" ]]; then
			python3 -m pip install --break-system-packages -r \"\${dir}/requirements.txt\"
		fi
	"
	log "Open Model Zoo git tag: ${omz_tag} (override with OPEN_MODEL_ZOO_TAG=…)"
}

run_once_step_agent_python_venv() {
	run_once_step "agent-python-venv" "
		set -euo pipefail
		python3 -m venv '${AGENT_VENV}'
		'${AGENT_VENV}/bin/pip' install -U pip wheel
		'${AGENT_VENV}/bin/pip' install \\
			autogen-agentchat \\
			crewai \\
			langgraph \\
			openai \\
			openai-agents
		if [[ '${INSTALL_PYTORCH_XPU}' == '1' ]]; then
			# PyTorch Intel GPU (XPU) backend; wheels bundle the needed runtime libs.
			'${AGENT_VENV}/bin/pip' install --index-url '${PYTORCH_XPU_INDEX_URL}' \\
				torch torchvision torchaudio
		fi
	"
}

RESOLVED_PACKAGES=()

# apt-cache search --names-only '^foo' is unreliable on some apt versions; use pkgnames + grep.
list_apt_pkg_names_matching() {
	local regex="$1"
	apt-cache pkgnames 2>/dev/null | grep -E "${regex}" || true
}

latest_apt_pkg_matching() {
	local regex="$1"
	local pkg="" f

	pkg="$(list_apt_pkg_names_matching "${regex}" | sort -V | tail -1)"
	if [[ -n "${pkg}" ]]; then
		echo "${pkg}"
		return 0
	fi

	while IFS= read -r f; do
		[[ -z "${f}" ]] && continue
		pkg="$(grep -E '^Package: ' "${f}" 2>/dev/null | awk '{print $2}' | grep -E "${regex}" | sort -V | tail -1)"
		if [[ -n "${pkg}" ]]; then
			echo "${pkg}"
			return 0
		fi
	done < <(find /var/lib/apt/lists -maxdepth 1 -type f -name '*_Packages' 2>/dev/null \
		| grep -E 'apt.repos.intel.com_(oneapi|edgeai|openvino)' | sort)

	echo ""
}

latest_openvino_meta_pkg() {
	local pkg="" f

	pkg="$(list_apt_pkg_names_matching '^openvino-[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)"
	if [[ -n "${pkg}" ]]; then
		echo "${pkg}"
		return 0
	fi

	while IFS= read -r f; do
		[[ -z "${f}" ]] && continue
		pkg="$(grep -E '^Package: openvino-[0-9]+\.[0-9]+\.[0-9]+$' "${f}" 2>/dev/null \
			| awk '{print $2}' | sort -V | tail -1)"
		if [[ -n "${pkg}" ]]; then
			echo "${pkg}"
			return 0
		fi
	done < <(find /var/lib/apt/lists -maxdepth 1 -type f -name '*openvino*Packages' 2>/dev/null | sort)

	return 1
}

latest_openvino_pkg() {
	latest_openvino_meta_pkg
}

openvino_meta_version() {
	local meta="$1"
	meta="${meta#openvino-}"
	echo "${meta}"
}

append_openvino_plugin_packages() {
	local ver="$1"
	local suffix pkg

	for suffix in hetero auto-batch auto intel-cpu intel-gpu intel-npu; do
		pkg="libopenvino-${suffix}-plugin-${ver}"
		if apt-cache show "${pkg}" >/dev/null 2>&1; then
			log "Intel OpenVINO plugin: ${pkg}"
			RESOLVED_PACKAGES+=("${pkg}")
		else
			log "WARN: optional OpenVINO plugin not in apt: ${pkg}"
		fi
	done
}

append_openvino_release_packages() {
	local ver="$1"
	local meta="openvino-${ver}"

	if ! apt-cache show "${meta}" >/dev/null 2>&1; then
		log "ERROR: ${meta} not in apt after resolve (internal error)"
		exit 1
	fi

	log "Intel OpenVINO meta: ${meta}"
	RESOLVED_PACKAGES+=("${meta}")
	append_openvino_plugin_packages "${ver}"
}

# Target OPENVINO_RELEASE when published; else optional fallback to newest openvino-* meta in apt.
resolve_openvino_apt_version() {
	local want="${OPENVINO_RELEASE}"
	local meta latest

	if apt-cache show "openvino-${want}" >/dev/null 2>&1; then
		echo "${want}"
		return 0
	fi

	if [[ "${OPENVINO_RELEASE_FALLBACK}" != "1" ]]; then
		log "ERROR: openvino-${want} not in apt (OPENVINO_RELEASE=${want}, track openvino/${OPENVINO_REPO_TRACK})"
		log "Hint: apt-cache pkgnames | grep -E '^openvino-' | sort -V | tail"
		log "Hint: set OPENVINO_RELEASE_FALLBACK=1 to install newest published meta until ${want} ships"
		exit 1
	fi

	meta="$(latest_openvino_meta_pkg)" || {
		log "ERROR: no openvino-* meta package in apt"
		exit 1
	}
	latest="$(openvino_meta_version "${meta}")"
	log "WARN: openvino-${want} not published on openvino/${OPENVINO_REPO_TRACK} ubuntu24 yet"
	log "WARN: OPENVINO_RELEASE_FALLBACK=1 — using ${meta} (newest in apt); re-run later for ${want}"
	echo "${latest}"
}

intel_openvino_repo_has_packages() {
	if apt-cache show "openvino-${OPENVINO_RELEASE}" >/dev/null 2>&1; then
		return 0
	fi
	latest_openvino_meta_pkg >/dev/null 2>&1
}

# Pinned names in PACKAGES may lag the live Intel repo; pick newest matching package.
resolve_pinned_apt_pkg() {
	local want="$1"
	local resolved=""

	if apt-cache show "${want}" >/dev/null 2>&1; then
		echo "${want}"
		return 0
	fi

	case "${want}" in
	openvino-*)
		if apt-cache show "${want}" >/dev/null 2>&1; then
			echo "${want}"
			return 0
		fi
		resolved="$(latest_openvino_meta_pkg || true)"
		;;
	openvino_*)
		resolved="$(latest_openvino_meta_pkg || true)"
		;;
	intel-oneapi-runtime-compilers_*)
		resolved="$(latest_apt_pkg_matching '^intel-oneapi-runtime-compilers_')"
		;;
	intel-oneapi-runtime-compilers-common_*)
		resolved="$(latest_apt_pkg_matching '^intel-oneapi-runtime-compilers-common_')"
		;;
	intel-oneapi-runtime-opencl_*)
		resolved="$(latest_apt_pkg_matching '^intel-oneapi-runtime-opencl_')"
		;;
	intel-dlstreamer_*)
		resolved="$(latest_apt_pkg_matching '^intel-dlstreamer_')"
		;;
	*)
		return 1
		;;
	esac

	if [[ -n "${resolved}" ]] && apt-cache show "${resolved}" >/dev/null 2>&1; then
		if [[ "${resolved}" != "${want}" ]]; then
			log "Package ${want} not in repo; using ${resolved}"
		fi
		echo "${resolved}"
		return 0
	fi
	return 1
}

resolve_packages_for_install() {
	RESOLVED_PACKAGES=()
	local pkg resolved pattern ver meta

	if [[ "${INTEL_PACKAGE_POLICY}" == "release" ]]; then
		ver="$(resolve_openvino_apt_version)"
		log "Intel package policy: release (target openvino-${OPENVINO_RELEASE}, installing openvino-${ver} + plugins + oneAPI + DL Streamer)"
		append_openvino_release_packages "${ver}"
		for pattern in "${INTEL_LATEST_APT_PATTERNS[@]}"; do
			resolved="$(latest_apt_pkg_matching "${pattern}")"
			if [[ -n "${resolved}" ]] && apt-cache show "${resolved}" >/dev/null 2>&1; then
				log "Intel release stack: ${pattern} -> ${resolved}"
				RESOLVED_PACKAGES+=("${resolved}")
			else
				log "WARN: no package for pattern ${pattern}"
			fi
		done
	elif [[ "${INTEL_PACKAGE_POLICY}" == "latest" ]]; then
		log "Intel package policy: latest (newest openvino-* meta from openvino/${OPENVINO_REPO_TRACK} + oneapi + dlstreamer)"
		if resolved="$(latest_openvino_meta_pkg)"; then
			ver="$(openvino_meta_version "${resolved}")"
			log "Intel latest: openvino meta -> ${resolved}"
			RESOLVED_PACKAGES+=("${resolved}")
			append_openvino_plugin_packages "${ver}"
		else
			log "WARN: no OpenVINO meta package found (try: apt-cache pkgnames | grep -E '^openvino-')"
		fi
		for pattern in "${INTEL_LATEST_APT_PATTERNS[@]}"; do
			resolved="$(latest_apt_pkg_matching "${pattern}")"
			if [[ -n "${resolved}" ]] && apt-cache show "${resolved}" >/dev/null 2>&1; then
				log "Intel latest: ${pattern} -> ${resolved}"
				RESOLVED_PACKAGES+=("${resolved}")
			else
				log "WARN: no package for pattern ${pattern}"
			fi
		done
	else
		log "Intel package policy: pinned (exact INTEL_PINNED_PACKAGES names)"
		for pkg in "${INTEL_PINNED_PACKAGES[@]}"; do
			if resolved="$(resolve_pinned_apt_pkg "${pkg}")"; then
				RESOLVED_PACKAGES+=("${resolved}")
				if [[ "${resolved}" =~ ^openvino-[0-9] ]]; then
					append_openvino_plugin_packages "$(openvino_meta_version "${resolved}")"
				fi
			else
				log "ERROR: pinned package missing: ${pkg}"
				exit 1
			fi
		done
	fi

	for pkg in "${PACKAGES[@]}"; do
		if apt-cache show "${pkg}" >/dev/null 2>&1; then
			RESOLVED_PACKAGES+=("${pkg}")
		else
			log "WARN: skipping unavailable package ${pkg}"
		fi
	done
}

log_installed_intel_versions() {
	local line
	while IFS= read -r line; do
		[[ -n "${line}" ]] && log "Installed: ${line}"
	done < <(dpkg-query -W -f='${Package} ${Version}\n' 'openvino*' 'intel-oneapi-runtime*' 'intel-dlstreamer*' 2>/dev/null || true)
}

installed_openvino_apt_release() {
	local pkg
	pkg="$(dpkg-query -W -f='${Package}\n' 'openvino-[0-9]*' 2>/dev/null \
		| grep -E '^openvino-[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)"
	[[ -n "${pkg}" ]] || return 1
	echo "${pkg#openvino-}"
}

omz_remote_tag_exists() {
	local tag="$1"
	git ls-remote --tags "${OPEN_MODEL_ZOO_GIT_URL}" "refs/tags/${tag}^{}" "refs/tags/${tag}" 2>/dev/null \
		| grep -q .
}

list_omz_remote_version_tags() {
	git ls-remote --tags "${OPEN_MODEL_ZOO_GIT_URL}" 2>/dev/null \
		| awk -F/ '{print $NF}' | sed 's/\^{}//' \
		| grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -Vu
}

# OPEN_MODEL_ZOO_TAG unset: match OPENVINO_RELEASE, then installed apt meta, else newest OMZ tag on GitHub.
resolve_open_model_zoo_tag() {
	local tag installed want newest

	if [[ -n "${OPEN_MODEL_ZOO_TAG}" ]]; then
		if omz_remote_tag_exists "${OPEN_MODEL_ZOO_TAG}"; then
			echo "${OPEN_MODEL_ZOO_TAG}"
			return 0
		fi
		log "ERROR: OPEN_MODEL_ZOO_TAG=${OPEN_MODEL_ZOO_TAG} not found on ${OPEN_MODEL_ZOO_GIT_URL}"
		return 1
	fi

	want="${OPENVINO_RELEASE}"
	installed="$(installed_openvino_apt_release || true)"
	for tag in "${want}" "${installed}"; do
		[[ -z "${tag}" ]] && continue
		if omz_remote_tag_exists "${tag}"; then
			if [[ "${tag}" != "${want}" ]]; then
				log "WARN: OMZ git tag ${want} not on GitHub; using ${tag} (aligned with OpenVINO apt / fallback)"
			fi
			echo "${tag}"
			return 0
		fi
	done

	newest="$(list_omz_remote_version_tags | tail -1)"
	if [[ -n "${newest}" ]]; then
		log "WARN: no OMZ tag matching OpenVINO ${want} (apt ${installed:-unknown}); using newest OMZ tag ${newest}"
		echo "${newest}"
		return 0
	fi

	log "ERROR: could not list version tags from ${OPEN_MODEL_ZOO_GIT_URL}"
	return 1
}

install_apt_packages() {
	if [[ ${#PACKAGES[@]} -eq 0 ]]; then
		log "No PACKAGES configured; skipping apt install"
		return 0
	fi

	export DEBIAN_FRONTEND=noninteractive
	export DEBCONF_NONINTERACTIVE_SEEN=true

	remove_ict_duplicate_intel_apt_lines

	log "apt-get update"
	apt-get update -y

	if ! intel_openvino_repo_has_packages; then
		log "ERROR: no OpenVINO package visible after apt update"
		log "Debug: apt-cache pkgnames | grep -i openvino | head"
		log "Check: cat /etc/apt/sources.list.d/intel-openvino.list (suite ubuntu24, track openvino/${OPENVINO_REPO_TRACK})"
		exit 1
	fi

	resolve_packages_for_install
	if [[ ${#RESOLVED_PACKAGES[@]} -eq 0 ]]; then
		log "ERROR: no installable packages resolved from PACKAGES list"
		exit 1
	fi

	log "apt-get install (${#RESOLVED_PACKAGES[@]} packages)"
	apt-get install -y --no-install-recommends "${RESOLVED_PACKAGES[@]}"
	log_installed_intel_versions
}

main() {
	require_root
	mkdir -p "$(dirname "${LOG_FILE}")" "${STAMP_DIR}" /opt/agent
	: >> "${LOG_FILE}"

	configure_network_proxy

	log "=== ${SCRIPT_NAME} start (FORCE=${FORCE:-1}, rev=${SCRIPT_REV}, OPENVINO_RELEASE=${OPENVINO_RELEASE}) ==="

	run_once_step_intel_apt_repos
	install_apt_packages
	run_once_step_open_model_zoo

	if [[ "${INSTALL_HERMES}" == "1" ]]; then
		run_once_step_hermes
	fi
	if [[ "${INSTALL_OPENCLAW}" == "1" ]]; then
		run_once_step_openclaw
	fi
	if [[ "${INSTALL_SUPERCLAW_CTL}" == "1" ]]; then
		run_once_step_superclaw_ctl
	fi

	run_once_step_agent_python_venv

	log "=== ${SCRIPT_NAME} complete ==="
	log "Python agent venv: ${AGENT_VENV}/bin/activate"
	if [[ "${INSTALL_PYTORCH_XPU}" == "1" ]]; then
		log "PyTorch XPU: ${AGENT_VENV}/bin/python -c 'import torch; print(torch.xpu.is_available())'"
	fi
	if [[ "${INSTALL_INTEL_GPU_REPO}" == "1" ]]; then
		log "Level Zero runtime: dpkg -l libze1 libze-intel-gpu1 | grep ^ii (from repositories.intel.com/gpu)"
	fi
	if [[ "${INSTALL_OPEN_MODEL_ZOO}" == "1" ]]; then
		log "Open Model Zoo: ${OPEN_MODEL_ZOO_DIR} (source /etc/profile.d/open-model-zoo.sh; see log for git tag)"
	fi
	if [[ "${INSTALL_HERMES}" == "1" ]]; then
		log "Hermes: configure manually — hermes setup (optional: hermes gateway install)"
	fi
	if [[ "${INSTALL_OPENCLAW}" == "1" ]]; then
		log "OpenClaw: run 'openclaw onboard' (or install daemon) when ready"
	fi
	if [[ "${INSTALL_SUPERCLAW_CTL}" == "1" ]]; then
		log "SuperClaw edge: superclaw-ctl — see Intel AI Builder superclaw-ctl USER-GUIDE"
	else
		log "SuperClaw desktop (Windows/WSL): https://github.com/intel/intel-ai-builder/tree/main/superclaw"
	fi
}

main "$@"
