#!/bin/bash
set -e

# Parse command line arguments
RUN_QEMU_TESTS=false
WORKING_DIR="$(pwd)"
# A payload deploy boot must hash/decompress/write/grow a whole disk image and
# reboot before "login:" ever appears, unlike a package-install ISO boot that
# only needs to reach the installer prompt - 30s (a plain-ISO-boot timeout)
# reports a normal payload deployment as a failure. Overridable since payload
# and target disk size vary per template.
DEPLOY_TIMEOUT=300

while [[ $# -gt 0 ]]; do
  case $1 in
    --qemu-test|--with-qemu)
      RUN_QEMU_TESTS=true
      shift
      ;;
    --working-dir)
      WORKING_DIR="$2"
      shift 2
      ;;
    --deploy-timeout)
      DEPLOY_TIMEOUT="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [--qemu-test|--with-qemu] [--working-dir DIR] [--deploy-timeout SECONDS]"
      echo "  --qemu-test, --with-qemu  Run QEMU boot tests after image build"
      echo "  --working-dir DIR         Set the working directory"
      echo "  --deploy-timeout SECONDS  Seconds to wait for login: after boot (default: 300)"
      echo "  -h, --help               Show this help message"
      exit 0
      ;;
    *)
      echo "Unknown option $1"
      echo "Use -h or --help for usage information"
      exit 1
      ;;
  esac
done

run_qemu_boot_test_iso() {
  local IMAGE_PATTERN="$1"
  if [ -z "$IMAGE_PATTERN" ]; then
    echo "Error: Image pattern not provided to run_qemu_boot_test_iso"
    return 1
  fi

  BIOS="/usr/share/OVMF/OVMF_CODE_4M.fd"
  TIMEOUT="$DEPLOY_TIMEOUT"
  SUCCESS_STRING="login:"
  LOGFILE="qemu_serial_iso.log"
  TARGET_DISK_SIZE="20G"

  ORIGINAL_DIR=$(pwd)
  # Find ISO image path using pattern, handle permission issues
  FOUND_PATH=$(sudo -S find . -type f -name "*${IMAGE_PATTERN}*.iso" 2>/dev/null | head -n 1)
  if [ -n "$FOUND_PATH" ]; then
    echo "Found ISO image at: $FOUND_PATH"
    IMAGE_DIR=$(dirname "$FOUND_PATH")

    cd "$IMAGE_DIR"

    ISO_IMAGE=$(basename "$FOUND_PATH")

    # QEMU is launched under sudo below, so check for the file as root too
    # rather than widening the build output's permissions to world-writable.
    if ! sudo -S test -f "$ISO_IMAGE"; then
      echo "Failed to find ISO image!"
      cd "$ORIGINAL_DIR"
      return 1
    fi

    IMAGE="$ISO_IMAGE"
  else
    echo "ISO image file matching pattern '*${IMAGE_PATTERN}*.iso' not found!"
    return 1
  fi

  # Deploy-mode payload ISOs need a target disk distinct from the read-only
  # installer media itself: live-installer's disk selection excludes
  # read-only iso9660 media, so without a separate disk it either finds no
  # eligible target or (if the ISO were misattached as another writable disk)
  # risks treating the installer media as the deploy target. Created fresh for
  # each run and removed on exit.
  TARGET_DISK=$(mktemp /tmp/ict_payload_target_XXXXXX.img)
  qemu-img create -f raw "$TARGET_DISK" "$TARGET_DISK_SIZE" >/dev/null
  trap 'rm -f "$TARGET_DISK"' RETURN

  echo "Booting ISO image: $IMAGE (target disk: $TARGET_DISK, $TARGET_DISK_SIZE)"
  # create log file, boot ISO image into qemu, return pass/fail after boot success
  sudo bash -c "
    LOGFILE=\"$LOGFILE\"
    SUCCESS_STRING=\"$SUCCESS_STRING\"
    IMAGE=\"$IMAGE\"
    TARGET_DISK=\"$TARGET_DISK\"
    ORIGINAL_DIR=\"$ORIGINAL_DIR\"
    TIMEOUT=\"$TIMEOUT\"

    # QEMU persists NVRAM changes to the writable pflash drive, so boot off a
    # per-run copy of the VARS template rather than the shared system file -
    # otherwise this contaminates it for every other VM/test on the host.
    VARS_COPY=\$(mktemp /tmp/ovmf_vars_XXXXXX.fd)
    cp /usr/share/OVMF/OVMF_VARS_4M.fd \"\$VARS_COPY\"

    touch \"\$LOGFILE\" && chmod 666 \"\$LOGFILE\"
    nohup qemu-system-x86_64 \\
        -m 2048 \\
        -enable-kvm \\
        -cpu host \\
        -device virtio-scsi-pci \\
        -drive if=none,id=installer-media,file=\"\$IMAGE\",format=raw,media=cdrom,readonly=on \\
        -device scsi-cd,drive=installer-media \\
        -drive if=none,id=target-disk,file=\"\$TARGET_DISK\",format=raw \\
        -device scsi-hd,drive=target-disk \\
        -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \\
        -drive if=pflash,format=raw,file=\"\$VARS_COPY\" \\
        -nographic \\
        -serial mon:stdio \\
        > \"\$LOGFILE\" 2>&1 &

    qemu_pid=\$!
    echo \"QEMU launched as root with PID \$qemu_pid\"
    echo \"Current working dir: \$(pwd)\"

    # Wait for SUCCESS_STRING or timeout
    elapsed=0
    while ! grep -q \"\$SUCCESS_STRING\" \"\$LOGFILE\" && [ \$elapsed -lt \$TIMEOUT ]; do
      sleep 1
      elapsed=\$((elapsed + 1))
    done
    echo \"\$elapsed\"
    kill \$qemu_pid
    cat \"\$LOGFILE\"

    if grep -q \"\$SUCCESS_STRING\" \"\$LOGFILE\"; then
      echo \"Boot success!\"
      result=0
    else
      echo \"Boot failed or timed out\"
      result=1
    fi

    rm -f \"\$VARS_COPY\"

    # Return to original directory
    cd \"\$ORIGINAL_DIR\"
    exit \$result
  "

  # Get the exit code from the sudo bash command
  qemu_result=$?
  return $qemu_result
}

cd "$WORKING_DIR"

git branch
# Build the ICT
echo "Building the ICT..."
echo "Generating binary with earthly..."
earthly +build

build_ubuntu24_installer_payload_iso_image() {
  echo "Building Ubuntu 24 installer-payload ISO image (using earthly built binary)"
  # Ensure we're in the working directory before starting builds
  echo "Ensuring we're in the working directory before starting builds..."
  cd "$WORKING_DIR"
  echo "Current working directory: $(pwd)"

  # Temporarily disable exit on error for the build command to capture output
  set +e
  output=$(sudo -S ./build/image-composer-tool build image-templates/ubuntu24/ubuntu24-x86_64-installer-payload-iso.yml 2>&1)
  build_exit_code=$?
  set -e

  # Check for the success message in the output. A payload-mode build emits
  # both the wrapped .iso and the standalone payload .raw from one invocation.
  if [ $build_exit_code -eq 0 ] && echo "$output" | grep -q "image build completed successfully"; then
    echo "Ubuntu 24 installer-payload ISO image build passed."
    if [ "$RUN_QEMU_TESTS" = true ]; then
      echo "Running QEMU boot test for Ubuntu 24 installer-payload ISO image..."
      if run_qemu_boot_test_iso "os-image-ubuntu-unattended-payload-24.04"; then
        echo "QEMU boot test PASSED for Ubuntu 24 installer-payload ISO image"
      else
        echo "QEMU boot test FAILED for Ubuntu 24 installer-payload ISO image"
        exit 1
      fi
    fi
  else
    echo "Ubuntu 24 installer-payload ISO image build failed."
    echo "Build output:"
    echo "$output"
    exit 1
  fi
}

# Run the main function
build_ubuntu24_installer_payload_iso_image
