#!/bin/sh
# Sample first-boot script for debian13-x86_64-bb-dracut-raw.yml.
# Runs once, on the device's first boot only (see first-boot-sample.service).
# set -eu: an unreported failure of any step here would still exit 0 (the
# final /dev/kmsg write almost always succeeds), which would make
# ExecStartPost write the done marker for an incomplete run. Fail fast
# instead so the unit reports failure and the next boot retries.
set -eu
msg="first boot sample: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "$msg" >/var/log/first-boot-sample.log
logger -t first-boot-sample "first boot sample script executed"
# Also mirror to /dev/kmsg so the message shows up in dmesg and on the serial
# console (this template's kernel cmdline sets console=ttyS0,115200), not just
# the journal/log file.
echo "$msg" >/dev/kmsg
