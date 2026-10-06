#!/bin/bash
# Sample first-boot provisioning script for the Base Platform Image.
# Runs once, after the network is online, from ict-provision-000-platform-bootstrap.service.
# Replace with the platform provisioning steps the deployment needs.
set -euo pipefail

marker_dir=/var/lib/image-composer-tool
mkdir -p "$marker_dir"
echo "platform bootstrap ran at $(date -Iseconds)" > "$marker_dir/platform-bootstrap.log"
