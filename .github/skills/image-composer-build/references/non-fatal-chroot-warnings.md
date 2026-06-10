# Non-Fatal Chroot Build Warnings

Builds that install `systemd-boot` inside a chroot environment reliably
produce this dpkg error — it is expected and harmless:

## symptom

```
Failed to write 'LoaderSystemToken' EFI variable: No such file or directory
dpkg: error processing package systemd-boot (--configure):
  installed systemd-boot package post-installation script subprocess
  returned error exit status 1
E: Sub-process /usr/bin/dpkg returned an error code (1)
```

## root cause

`systemd-boot`'s post-install script tries to write to EFI variables
(`/sys/firmware/efi/efivars/`), which do not exist inside a chroot
environment. This is not a packaging bug — the variable write is an
optimization (seeding `LoaderSystemToken`), not required for boot.

## why it's safe

1. The image-composer-tool logs this at **INFO** level (not ERROR/WARN)
   and continues the build.
2. The final bootloader installation happens in a separate step after
   the chroot environment is torn down.
3. **Actual builds that hit this:** Ubuntu 24.04 + ROS2 Jazzy raw image
   (`robotics-demo` and `ros2-with-tools` variants). Both built to
   completion with exit code 0 and produced bootable images.

## how to confirm the build is healthy

If you see this error, check:

```bash
# Was the overall exit code 0?
echo $?

# Did the build summary show "IMAGE CREATED SUCCESSFULLY"?
grep -q "IMAGE CREATED SUCCESSFULLY" <build-log>

# Does the artifact exist?
ls -lh workspace/*/imagebuild/*/*.raw.gz 2>/dev/null
```

If all three pass, the image is good. The dpkg error can be safely
ignored.

## other chroot warnings

| Warning | Cause | Impact |
|---------|-------|--------|
| `perl: warning: Setting locale failed` | `LC_*` env vars set to non-english locales not installed in chroot | None — falls back to C locale |
| `Failed to write 'LoaderSystemToken' EFI variable` | EFI vars unavailable in chroot | None — bootloader installed post-chroot |
