#!/bin/bash
# First-boot provisioning script for the Edge Node Infrastructure Blueprint.
# Runs once, after the network is online, from
# ict-provision-NNN-edge-node-infra-setup.service, by which point the proxy
# baked into the image (/etc/systemd/system.conf.d/90-ict-proxy.conf,
# /etc/environment) is already in effect — unlike systemConfig.configurations,
# which runs during image install, before that proxy config is written.
set -euo pipefail

# Container runtime to provision: "docker" or "k3s".
deployment_mode=docker

repo_dir=/opt/edge-node-infrastructure-blueprint

git clone https://github.com/open-edge-platform/edge-node-infrastructure-blueprint.git "$repo_dir"
cd "$repo_dir"
git checkout enib-packages-installer

case "$deployment_mode" in
docker)
	make docker
	;;
k3s)
	make k3s
	;;
*)
	echo "unsupported deployment_mode: $deployment_mode (expected 'docker' or 'k3s')" >&2
	exit 1
	;;
esac

# Provisioning needs a reboot to take effect; warn any logged-in users, then reboot.
msg="Edge Node Infrastructure provisioning complete. Rebooting in 1 minute to apply changes."
echo "$msg"
shutdown -r +1 "$msg"
