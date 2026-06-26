#!/bin/bash
# Install Intel agent stack + common agent frameworks on Ubuntu 22.04 / 24.04.
# Target: x86_64 with Intel GPU/NPU (set INTEL_APT_ARCH=arm64 on aarch64 hosts).
#
# Intel apt suites are ubuntu22 / ubuntu24 (not jammy/noble). Override:
#   INTEL_UBUNTU_SUITE=ubuntu22|ubuntu24
#
# Usage:
#   sudo /opt/agent/agent-install.sh
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
#   INTEL_PACKAGE_POLICY=latest   # latest | pinned (exact deb names below)
#   OPENVINO_REPO_TRACK=2025       # Intel apt path …/openvino/${OPENVINO_REPO_TRACK}
#   OPENCLAW_INSTALL_URL=https://openclaw.ai/install.sh
#   SUPERCLAW_CTL_URL=…/superclaw-ctl-v1.0.0-linux-x86-64.tar.gz
#   SUPERCLAW_CTL_PREFIX=/opt/superclaw
#   HERMES_INSTALL_FLAGS="--skip-setup --non-interactive --skip-browser"  # skips Playwright only; Hermes may still install Node
#   HERMES_INSTALL_AS_USER=         # optional; empty = install as script user (root → /usr/local/…)
#
# Rerunnable: apt-get update/install every run; stamped custom steps every run (FORCE=1 default).
# Set FORCE=0 to skip completed stamp steps. Requires: network, root, writable apt/dpkg.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
readonly SCRIPT_REV="2026-06-26-intel-ubuntu-suite-v8"
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

INSTALL_HERMES="${INSTALL_HERMES:-1}"
INSTALL_OPENCLAW="${INSTALL_OPENCLAW:-1}"
INSTALL_SUPERCLAW_CTL="${INSTALL_SUPERCLAW_CTL:-0}"
INTEL_PACKAGE_POLICY="${INTEL_PACKAGE_POLICY:-latest}"

# Used only when INTEL_PACKAGE_POLICY=pinned (ICT template defaults; may not match live apt).
INTEL_PINNED_PACKAGES=(
	openvino_2025.3.0.19807
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
	echo "[${LOG_TAG}] $(date -u +%Y-%m-%dT%H:%M:%SZ) $*" | tee -a "${LOG_FILE}"
}

require_root() {
	if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
		echo "${SCRIPT_NAME}: run as root (e.g. sudo ${SCRIPT_NAME})" >&2
		exit 1
	fi
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

intel_apt_sources_ok() {
	local suite
	suite="$(detect_intel_ubuntu_suite)" || return 1
	[[ -f /etc/apt/sources.list.d/intel-openvino.list ]] || return 1
	grep -qF "openvino/${OPENVINO_REPO_TRACK} ${suite} main" /etc/apt/sources.list.d/intel-openvino.list || return 1
	[[ -f /etc/apt/sources.list.d/intel-dlstreamer.list ]] || return 1
	grep -qF "dlstreamer/${suite} ${suite} main" /etc/apt/sources.list.d/intel-dlstreamer.list || return 1
	[[ -f /etc/apt/sources.list.d/intel-oneapi.list ]] || return 1
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
	configure_intel_apt_repos_files "${suite}" "${dls_base}"
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

latest_openvino_pkg() {
	local pkg="" lists="" f

	pkg="$(list_apt_pkg_names_matching '^openvino_' | sort -V | tail -1)"
	if [[ -n "${pkg}" ]]; then
		echo "${pkg}"
		return 0
	fi

	if apt-cache show openvino >/dev/null 2>&1; then
		echo "openvino"
		return 0
	fi

	pkg="$(list_apt_pkg_names_matching '^openvino' | sort -V | tail -1)"
	if [[ -n "${pkg}" ]] && apt-cache show "${pkg}" >/dev/null 2>&1; then
		echo "${pkg}"
		return 0
	fi

	while IFS= read -r f; do
		[[ -z "${f}" ]] && continue
		pkg="$(grep -E '^Package: openvino' "${f}" 2>/dev/null | awk '{print $2}' | sort -V | tail -1)"
		if [[ -n "${pkg}" ]]; then
			echo "${pkg}"
			return 0
		fi
	done < <(find /var/lib/apt/lists -maxdepth 1 -type f -name '*openvino*Packages' 2>/dev/null | sort)

	return 1
}

intel_openvino_repo_has_packages() {
	latest_openvino_pkg >/dev/null 2>&1
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
	openvino_*)
		resolved="$(latest_openvino_pkg || true)"
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
	local pkg resolved pattern

	if [[ "${INTEL_PACKAGE_POLICY}" == "latest" ]]; then
		log "Intel package policy: latest (newest in apt from openvino/${OPENVINO_REPO_TRACK} + oneapi + dlstreamer repos)"
		if resolved="$(latest_openvino_pkg)"; then
			log "Intel latest: openvino -> ${resolved}"
			RESOLVED_PACKAGES+=("${resolved}")
		else
			log "WARN: no OpenVINO package found (try: apt-cache pkgnames | grep -i openvino)"
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

	log "=== ${SCRIPT_NAME} start (FORCE=${FORCE:-1}, rev=${SCRIPT_REV}) ==="

	run_once_step_intel_apt_repos
	install_apt_packages

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
