#!/usr/bin/env bash
#
# bare-metal-bringup.sh — first-/every-boot bring-up for a Debian cloud image
# flashed to bare metal, where there is NO cloud-init datasource.
#
# The Debian generic cloud image expects cloud-init to configure networking and
# SSH from a metadata datasource at first boot. On bare metal there is no
# datasource, so networking and sshd never come up — the device gets no IP and
# SSH never starts. This script performs that bring-up directly:
#
#   1. DHCP every wired NIC via systemd-networkd (always present with systemd),
#   2. generate PER-DEVICE SSH host keys, enable password auth, start sshd.
#
# It is idempotent — safe to run repeatedly (the MODE A oneshot runs it on every
# boot, and it is safe to re-run by hand). Only MODE A actually runs it (see the
# image template's MODE toggle):
#   * MODE A (no cloud-init): run by the bare-metal-bringup.service systemd oneshot.
#   * MODE B (cloud-init):    shipped but left inert; cloud-init provisions
#                             networking/SSH from the datasource instead (no local
#                             NoCloud seed is baked — it would shadow that datasource).
#
# This script is deliberately MODE-AGNOSTIC: it does NOT touch cloud-init. Whether
# cloud-init is disabled is the template's decision (MODE A disables it at build;
# MODE B keeps it enabled), so the same script is correct under both invokers.
# Run it by hand for recovery:  sudo bare-metal-bringup.sh
#
# INSECURE BRING-UP: this enables PASSWORD SSH auth. Before any real deployment,
# switch to key-based auth and remove /etc/ssh/sshd_config.d/00-baremetal.conf.
#
# It also does NOT create a login user — the login account is provisioned by the
# image template (systemConfig.users). Without a user, password SSH has nothing
# to log into.

# Keep going if an individual step fails (no `set -e`), but REMEMBER failures of
# required steps in `rc` so the oneshot still exits nonzero — otherwise a broken
# bring-up (no IP / no SSH) would report success under RemainAfterExit=yes.
# Optional steps (systemd-resolved, unit enablement) stay best-effort.
set -uo pipefail

rc=0
log()  { echo "bare-metal-bringup: $*"; }
fail() { log "ERROR: $*"; rc=1; }

# --- 1. Networking: DHCP on every wired interface ----------------------------
# systemd-networkd ships with systemd, so this works without ifupdown / netplan /
# NetworkManager being installed. Match every wired NIC by DEVICE TYPE (Type=ether)
# rather than by name prefix: predictable names are not guaranteed to start with
# `en` (e.g. `eth0` under net.ifnames=0, or vendor/board names), and Type=ether
# still excludes loopback and WiFi (Type=wlan).
#
# ClientIdentifier=mac is REQUIRED for bare metal: by default networkd sends an
# RFC-4361 DUID as the DHCP client id, but many bare-metal DHCP servers lease by
# MAC (reservations / per-MAC pools) and ignore the DUID — so you get no lease or
# a different address than expected. Sending the raw MAC matches those servers.
# IPv6AcceptRA=no stops the NIC coming "up" with only an RA-derived IPv6 address
# and no usable IPv4. (QEMU's slirp DHCP leases regardless, which masked this.)
#
# Caveat (MODE B): if cloud-init is left enabled and also renders a network
# config, both request DHCP on the same NIC — harmless (they converge on the same
# lease), but for a real deployment pick ONE network manager.
mkdir -p /etc/systemd/network || fail "could not create /etc/systemd/network"
cat > /etc/systemd/network/20-baremetal-dhcp.network <<'EOF' || fail "could not write DHCP .network drop-in"
# Bare-metal bring-up: DHCP every wired NIC. Replace with a static or otherwise
# managed configuration for a real deployment.
[Match]
Type=ether

[Network]
DHCP=yes
IPv6AcceptRA=no

[DHCPv4]
SendRelease=false
ClientIdentifier=mac
EOF

systemctl enable systemd-networkd >/dev/null 2>&1 || true
# Restart (not just start) so a config rewritten on a later boot is re-read.
# REQUIRED: without networkd running there is no DHCP, so a failure fails the unit.
systemctl restart systemd-networkd 2>/dev/null || systemctl start systemd-networkd 2>/dev/null || fail "systemd-networkd did not (re)start"
# DNS resolution — OPTIONAL and harmless if the unit is absent (best-effort).
systemctl enable systemd-resolved >/dev/null 2>&1 || true
systemctl start systemd-resolved 2>/dev/null || true
log "systemd-networkd configured for DHCP"

# --- 2. SSH: per-device host keys + password auth + service ------------------
# /run is a fresh tmpfs each boot; sshd's privilege-separation dir must exist.
mkdir -p /run/sshd || fail "could not create /run/sshd"

# ssh-keygen -A generates any MISSING host keys under /etc/ssh — it does NOT
# overwrite existing ones. Keys are UNIQUE per unit only if the build shipped
# none: the MODE A configurations step removes any host keys baked by the
# openssh-server install (otherwise every flashed unit would share them — a
# fleet-wide MITM risk). First boot regenerates them here; later boots find them
# present and leave the device identity unchanged (idempotent).
ssh-keygen -A || fail "ssh-keygen -A failed to generate host keys"
log "host keys ensured"

# Force password auth on for bring-up. A 00- drop-in wins over the baseline's
# "PasswordAuthentication no" (the Include sits above that line and sshd takes
# the first value it sees per option); we also rewrite the main line to remove
# any ambiguity.
mkdir -p /etc/ssh/sshd_config.d || fail "could not create /etc/ssh/sshd_config.d"
cat > /etc/ssh/sshd_config.d/00-baremetal.conf <<'EOF' || fail "could not write sshd_config.d drop-in"
PasswordAuthentication yes
PermitRootLogin no
EOF
if [ -f /etc/ssh/sshd_config ]; then
	sed -i 's/^[#[:space:]]*PasswordAuthentication[[:space:]].*/PasswordAuthentication yes/' /etc/ssh/sshd_config || true
fi
log "password SSH auth enabled"

# Debian's unit is named "ssh" (not "sshd"). Enable for future boots (best-effort;
# the oneshot restarts ssh every boot anyway), start now.
systemctl enable ssh >/dev/null 2>&1 || true
# REQUIRED: without sshd running there is no login, so a failure fails the unit.
systemctl restart ssh 2>/dev/null || systemctl start ssh 2>/dev/null || fail "ssh did not (re)start"
log "sshd enabled and started"

if [ "$rc" -ne 0 ]; then
	log "bring-up completed WITH ERRORS — see the ERROR lines above"
else
	log "bring-up complete"
fi
exit "$rc"
