#!/bin/bash
# First-boot provisioning script for the Edge Node Infrastructure Blueprint.
# Runs once, after the network is online, from
# ict-provision-NNN-edge-node-infra-setup.service, by which point the proxy
# baked into the image (/etc/systemd/system.conf.d/90-ict-proxy.conf,
# /etc/environment) is already in effect — unlike systemConfig.configurations,
# which runs during image install, before that proxy config is written.
#
# The Fed Aero templates set systemConfig.provisioning.beforeLogin, so this runs
# before the login prompt: console and SSH logins are unavailable until it has
# finished. They also set rebootAfter, so the unit reboots the system once this
# script succeeds and the login prompt never appears on the provisioning boot.
#
# Usage: edge-node-infra-setup.sh [--mode docker|k3s]
# The template chooses the container runtime to provision with
# systemConfig.provisioning.scripts[].args, for example args: ["--mode", "k3s"];
# without an argument it provisions docker.
set -euo pipefail

deployment_mode=docker
while (($# > 0)); do
	case "$1" in
	--mode)
		if (($# < 2)); then
			echo "--mode needs a value (usage: $0 [--mode docker|k3s])" >&2
			exit 2
		fi
		deployment_mode="$2"
		shift 2
		;;
	--mode=*)
		deployment_mode="${1#--mode=}"
		shift
		;;
	*)
		echo "unknown argument: $1 (usage: $0 [--mode docker|k3s])" >&2
		exit 2
		;;
	esac
done
# Fail before any network work rather than after the clone.
case "$deployment_mode" in
docker | k3s) ;;
*)
	echo "unsupported deployment mode: $deployment_mode (expected 'docker' or 'k3s')" >&2
	exit 2
	;;
esac

# Intel's corporate proxy (proxy-dmz.intel.com) TLS-intercepts and re-signs
# outbound HTTPS with Intel's internal CA chain, which isn't in the public
# ca-certificates bundle, so curl (used by download-resources.sh for the
# Docker/K3s static binaries) fails with "unable to get local issuer
# certificate" unless Intel's Root CA certificates are installed first.
#
# Detection is scoped to an *.intel.com proxy host, not "any proxy
# configured": systemConfig.proxy's documented example
# (http://proxy.example.com:3128) has no reason to need or serve Intel's
# certificates, so treating it the same would add a build-breaking network
# dependency on certificates.intel.com to an unrelated proxy setup. The
# systemd proxy env (DefaultEnvironment, see
# /etc/systemd/system.conf.d/90-ict-proxy.conf) is what makes
# https_proxy/HTTPS_PROXY visible here.
# proxy_host prints the host of the configured proxy, or nothing if none is set.
# A bracketed IPv6 literal (http://[2001:db8::1]:3128) is printed without its
# brackets, which is the form getent and curl's --noproxy take.
proxy_host() {
	local proxy_url host
	proxy_url="${https_proxy:-${HTTPS_PROXY:-${http_proxy:-${HTTP_PROXY:-}}}}"
	[[ -n "$proxy_url" ]] || return 0
	host="${proxy_url#*://}"
	host="${host##*@}" # strip optional user:pass@ credentials before the port, not after
	host="${host%%/*}"
	if [[ "$host" == \[* ]]; then
		host="${host#\[}"
		host="${host%%]*}"
	else
		host="${host%%:*}"
	fi
	echo "$host"
}

is_intel_corporate_proxy() {
	local host
	host="$(proxy_host)"
	[[ -n "$host" ]] || return 1
	case "${host,,}" in
	*.intel.com | intel.com) return 0 ;;
	*) return 1 ;;
	esac
}

# wait_for_dns blocks until every given host resolves, for up to 2 minutes of
# wall-clock time each, then warns and carries on (the network calls that
# follow are retried and report the real error). The deadline bounds the
# lookups as well as the sleeps: with an unreachable DNS server a single
# getent can itself block for many seconds, so each lookup is limited to the
# time left. It is deliberately not limited to a shorter fixed cap, which
# would kill a slow but successful resolution (for example one failing over
# from an unreachable primary DNS server). network-online.target only means a
# link is configured, so name resolution can lag behind it at first boot.
wait_for_dns() {
	local host deadline remaining
	for host in "$@"; do
		deadline=$((SECONDS + 120))
		while ((deadline - SECONDS > 0)); do
			remaining=$((deadline - SECONDS))
			# ahosts goes through getaddrinfo, the lookup git and curl use, and
			# accepts an IP literal as is; "hosts" does a reverse lookup for one
			# and fails without a PTR entry although no DNS is needed.
			if timeout "$remaining" getent ahosts "$host" >/dev/null 2>&1; then
				continue 2
			fi
			remaining=$((deadline - SECONDS))
			echo "Waiting for DNS to resolve $host (${remaining}s left)" >&2
			((remaining > 0)) && sleep "$((remaining < 2 ? remaining : 2))"
		done
		echo "warning: $host still does not resolve; continuing anyway" >&2
	done
}

# retry runs a command up to $1 times, pausing between attempts, and fails
# with the command's status if every attempt fails.
retry() {
	local attempts="$1" attempt rc=0
	shift
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		"$@" && return 0
		rc=$?
		echo "attempt $attempt/$attempts failed (exit $rc): $*" >&2
		((attempt < attempts)) && sleep 10
	done
	return "$rc"
}

# validate_intel_ca_cert rejects anything that isn't an unexpired, Intel-named
# CA certificate before it is trusted. This is a partial mitigation, not a
# substitute for pinning a checksum/signature: Intel's own distribution point
# for this bundle (certificates.intel.com) does not publish one to pin
# against, so a sophisticated on-path attacker able to forge a well-formed,
# Intel-named CA certificate would still pass these checks. It does catch a
# truncated, corrupted, or obviously-wrong (e.g. HTML error page) response.
validate_intel_ca_cert() {
	local cert="$1"
	if ! openssl x509 -in "$cert" -noout -checkend 0 >/dev/null 2>&1; then
		echo "error: $cert is not a valid, unexpired X.509 certificate; refusing to trust it" >&2
		return 1
	fi
	if ! openssl x509 -in "$cert" -noout -subject 2>/dev/null | grep -qi intel; then
		echo "error: $cert's subject does not name Intel; refusing to trust it" >&2
		return 1
	fi
	if ! openssl x509 -in "$cert" -noout -ext basicConstraints 2>/dev/null | grep -qi 'CA:TRUE'; then
		echo "error: $cert is not marked as a CA certificate; refusing to trust it" >&2
		return 1
	fi
}

# install_validated_certs copies every validated *.crt found under $1 into the
# trust anchor directory and refreshes the system trust store. A cert that
# fails validate_intel_ca_cert is skipped, not fatal: Intel's own official
# root chain bundle legitimately ships superseded intermediate CA certs
# alongside current ones (kept for compatibility with certs issued under the
# old intermediate), so one expired entry in an otherwise-good bundle must
# not block installing the rest. Exits via set -e if no *.crt files are found
# at all, or if every one found fails validation (silently proceeding with no
# certs installed would leave later downloads failing the same way this
# function exists to prevent, just less visibly).
install_validated_certs() {
	local src_dir="$1" cert_dir=/usr/local/share/ca-certificates/intel found=0 installed=0 cert
	mkdir -p "$cert_dir"
	while IFS= read -r -d '' cert; do
		found=1
		if ! validate_intel_ca_cert "$cert"; then
			continue
		fi
		cp "$cert" "$cert_dir/"
		installed=1
	done < <(find "$src_dir" -name '*.crt' -print0)
	if [[ "$found" -eq 0 ]]; then
		echo "error: no certificate files found under $src_dir" >&2
		exit 1
	fi
	if [[ "$installed" -eq 0 ]]; then
		echo "error: no certificate files under $src_dir passed validation; refusing to proceed" >&2
		exit 1
	fi
	update-ca-certificates
}

install_intel_root_ca_certificates() {
	is_intel_corporate_proxy || return 0

	echo "Intel corporate proxy detected; installing Intel Root CA certificates"

	local stage
	stage="$(mktemp -d)"
	trap 'rm -rf "$stage"' RETURN

	# --noproxy: certificates.intel.com is Intel's own certificate
	# distribution point, not a third-party site the corporate proxy needs
	# to TLS-intercept. Routed through proxy-dmz, this host falls through to
	# an unrelated default backend and 404s instead of reaching the real
	# server; the fix is to bypass the proxy for it, matching the existing
	# "Acquire::https::proxy::*.intel.com DIRECT" convention used for apt
	# elsewhere in this repo.
	#
	# Plain HTTP: fetching the CA chain itself must not depend on the CA
	# chain it installs. validate_intel_ca_cert (via install_validated_certs)
	# is this download's only protection against a corrupted or substituted
	# response — see its comment for what it does and does not catch.
	curl -fsSL --noproxy certificates.intel.com -o "$stage/IntelSHA2RootChain-Base64.zip" \
		http://certificates.intel.com/repository/certificates/IntelSHA2RootChain-Base64.zip
	unzip -oq "$stage/IntelSHA2RootChain-Base64.zip" -d "$stage"
	install_validated_certs "$stage"
	# Clear the extracted chain out so the next install_validated_certs call
	# (after the download below) only processes the new file, not this
	# batch again.
	find "$stage" -name '*.crt' -delete

	# Also bypasses the proxy, for the same reason as above. Going direct
	# reaches certificates.intel.com's own certificate (publicly trusted,
	# not the corporate proxy's re-signed one), so this keeps full
	# certificate validation against the default trust store rather than
	# depending on the SHA2 chain just installed above.
	curl -fsSL --noproxy certificates.intel.com -o "$stage/IntelSHA384RootCA-Base64.crt" \
		https://certificates.intel.com/repository/certificates/IntelSHA384RootCA-Base64.crt
	install_validated_certs "$stage"
}

# The proxy host is resolved by the clone and downloads below; the certificate
# host by the Intel Root CA download (which bypasses the proxy). Resolving
# them is racy at first boot, so wait rather than fail the whole script.
dns_hosts=(github.com)
if [[ -n "$(proxy_host)" ]]; then
	dns_hosts=("$(proxy_host)")
fi
if is_intel_corporate_proxy; then
	dns_hosts+=(certificates.intel.com)
fi
wait_for_dns "${dns_hosts[@]}"

install_intel_root_ca_certificates

# ENIB's build (make docker/k3s) compiles Go tooling (e.g. the CDI GPU spec
# generator), which needs a module cache location. This script runs as root
# via a bare oneshot systemd unit with no User= and no PAM session, so $HOME
# is never populated the way an interactive root login would set it, leaving
# go with neither $HOME, $GOPATH, nor $GOMODCACHE to default from ("neither
# GOMODCACHE nor GOPATH is set"). Set it explicitly so go can resolve its
# default module cache under $HOME/go/pkg/mod.
export HOME=/root

repo_dir=/opt/edge-node-infrastructure-blueprint

# A previous failed attempt (e.g. make failing after a good clone) leaves
# $repo_dir behind, and the unit is retried on the next boot because no stamp
# file is written on failure; git clone would then refuse the existing
# directory. Start from a clean checkout so the retry can actually succeed.
rm -rf "$repo_dir"
retry 5 git clone https://github.com/open-edge-platform/edge-node-infrastructure-blueprint.git "$repo_dir"
cd "$repo_dir"
git checkout enib-packages-installer

# deployment_mode is validated above and names the matching ENIB make target.
make "$deployment_mode"

# The unit reboots the system after this script succeeds (rebootAfter), which
# applies the changes.
echo "Edge Node Infrastructure provisioning complete."
