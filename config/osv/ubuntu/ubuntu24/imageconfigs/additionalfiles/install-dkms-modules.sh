#!/bin/bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

echo "Installing DKMS packages from /etc/dkms-packages.list..."

if [[ ! -f /etc/dkms-packages.list ]]; then
  echo "No DKMS packages list found. Skipping installation."
  exit 0
fi

apt-get update || { echo "Failed to update package list"; exit 1; }

while IFS= read -r package || [[ -n "$package" ]]; do
  # Skip empty lines and comments
  if [[ -z "$package" ]] || [[ "$package" =~ ^[[:space:]]*# ]]; then
    continue
  fi
  
  # Remove leading/trailing whitespace
  package=$(echo "$package" | xargs)
  
  if [[ -n "$package" ]]; then
    echo "Installing $package..."
    if apt-get install -y "$package"; then
      echo "✓ Successfully installed $package"
    else
      echo "✗ Failed to install $package"
    fi
  fi
done < /etc/dkms-packages.list

echo "DKMS installation complete."
echo "Cleaning up installation service..."
systemctl disable install-dkms-modules.service || true
rm -f /etc/systemd/system/install-dkms-modules.service
rm -f /usr/local/bin/install-dkms-modules.sh
rm -f /etc/dkms-packages.list
echo "Cleanup complete."