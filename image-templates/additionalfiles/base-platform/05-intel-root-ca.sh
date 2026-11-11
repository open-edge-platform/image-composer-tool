#!/bin/bash
# First-boot provisioning script: trust Intel's Root CA certificates behind Intel's
# corporate proxy. Runs before the OEP Installer unit, whose K3s, Docker and Go
# downloads are HTTPS and otherwise fail with "unable to get local issuer
# certificate", because the proxy (proxy-dmz.intel.com) TLS-intercepts and
# re-signs traffic with Intel's internal CA chain.
#
# Does nothing unless the configured proxy host is under intel.com, so the image
# is unaffected on any other network. The proxy variables come from the systemd
# default environment ICT writes from systemConfig.proxy.
set -euo pipefail

proxy_host() {
	local proxy_url host
	proxy_url="${https_proxy:-${HTTPS_PROXY:-${http_proxy:-${HTTP_PROXY:-}}}}"
	[[ -n "$proxy_url" ]] || return 0
	host="${proxy_url#*://}"
	host="${host##*@}"
	host="${host%%/*}"
	if [[ "$host" == \[* ]]; then
		host="${host#\[}"
		host="${host%%]*}"
	else
		host="${host%%:*}"
	fi
	echo "$host"
}

host="$(proxy_host)"
case "${host,,}" in
*.intel.com | intel.com) ;;
*)
	echo "No Intel corporate proxy configured; not installing Intel Root CA certificates"
	exit 0
	;;
esac

# Reject anything that is not an unexpired, Intel-named CA certificate. Intel's
# chain bundle also ships superseded intermediates, so a failing certificate is
# skipped; the script fails only if none passes.
valid_intel_ca() {
	openssl x509 -in "$1" -noout -checkend 0 >/dev/null 2>&1 &&
		openssl x509 -in "$1" -noout -subject 2>/dev/null | grep -qi intel &&
		openssl x509 -in "$1" -noout -ext basicConstraints 2>/dev/null | grep -qi 'CA:TRUE'
}

install_valid_certs() {
	local cert_dir=/usr/local/share/ca-certificates/intel cert installed=0
	mkdir -p "$cert_dir"
	while IFS= read -r -d '' cert; do
		if valid_intel_ca "$cert"; then
			cp "$cert" "$cert_dir/"
			installed=1
		else
			echo "skipping $cert: not a valid, unexpired Intel CA certificate" >&2
		fi
	done < <(find "$1" -name '*.crt' -print0)
	if [[ "$installed" -eq 0 ]]; then
		echo "error: no certificate under $1 passed validation" >&2
		exit 1
	fi
}

# certificates.intel.com is Intel's own distribution point; it is reached directly
# because the proxy does not forward it. The chain archive is fetched over plain
# HTTP so that downloading it does not depend on the chain it installs, and is
# validated above. python3 extracts it, since the images do not install unzip.
echo "Intel corporate proxy ($host) detected; installing Intel Root CA certificates"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

base=certificates.intel.com/repository/certificates
for attempt in 1 2 3 4 5; do
	curl -fsSL --noproxy certificates.intel.com -o "$stage/chain.zip" "http://$base/IntelSHA2RootChain-Base64.zip" && break
	[[ "$attempt" -lt 5 ]] || exit 1
	echo "attempt $attempt failed; retrying" >&2
	sleep 10
done
python3 -I -m zipfile -e "$stage/chain.zip" "$stage/chain"
install_valid_certs "$stage/chain"

mkdir -p "$stage/sha384"
curl -fsSL --noproxy certificates.intel.com -o "$stage/sha384/IntelSHA384RootCA-Base64.crt" \
	"https://$base/IntelSHA384RootCA-Base64.crt"
install_valid_certs "$stage/sha384"

update-ca-certificates
