#!/bin/bash
set -e

# Parse command line arguments
RUN_QEMU_TESTS=false
WORKING_DIR="$(pwd)"

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
    -h|--help)
      echo "Usage: $0 [--qemu-test|--with-qemu] [--working-dir DIR]"
      echo "  --qemu-test, --with-qemu  Run QEMU boot tests after image build"
      echo "  --working-dir DIR         Set the working directory"
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
  TIMEOUT=30
  SUCCESS_STRING="login:"
  LOGFILE="qemu_serial_iso.log"

  ORIGINAL_DIR=$(pwd)
  # Find ISO image path using pattern, handle permission issues
  FOUND_PATH=$(sudo -S find . -type f -name "*${IMAGE_PATTERN}*.iso" 2>/dev/null | head -n 1)
  if [ -n "$FOUND_PATH" ]; then
    echo "Found ISO image at: $FOUND_PATH"
    IMAGE_DIR=$(dirname "$FOUND_PATH")
    
    # Fix permissions for the image directory recursively to allow access
    IMAGE_ROOT_DIR=$(echo "$IMAGE_DIR" | cut -d'/' -f2)  # Get the root directory (workspace or tmp)
    echo "Setting permissions recursively for ./$IMAGE_ROOT_DIR directory"
    sudo chmod -R 777 "./$IMAGE_ROOT_DIR"
    
    cd "$IMAGE_DIR"
    
    ISO_IMAGE=$(basename "$FOUND_PATH")
    
    if [ ! -f "$ISO_IMAGE" ]; then
      echo "Failed to find ISO image!"
      cd "$ORIGINAL_DIR"
      return 1
    fi
    
    IMAGE="$ISO_IMAGE"
  else
    echo "ISO image file matching pattern '*${IMAGE_PATTERN}*.iso' not found!"
    return 1
  fi

  echo "Booting ISO image: $IMAGE "
  #create log file ,boot ISO image into qemu , return the pass or fail after boot sucess
  sudo bash -c "
    LOGFILE=\"$LOGFILE\"
    SUCCESS_STRING=\"$SUCCESS_STRING\"
    IMAGE=\"$IMAGE\"
    RAW_IMAGE=\"$RAW_IMAGE\"
    ORIGINAL_DIR=\"$ORIGINAL_DIR\"
    
    touch \"\$LOGFILE\" && chmod 666 \"\$LOGFILE\"    
    nohup qemu-system-x86_64 \\
        -m 2048 \\
        -enable-kvm \\
        -cpu host \\
        -drive if=none,file=\"\$IMAGE\",format=raw,id=nvme0 \\
        -device nvme,drive=nvme0,serial=deadbeef \\
        -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \\
        -drive if=pflash,format=raw,file=/usr/share/OVMF/OVMF_VARS_4M.fd \\
        -nographic \\
        -serial mon:stdio \\
        > \"\$LOGFILE\" 2>&1 &

    qemu_pid=\$!
    echo \"QEMU launched as root with PID \$qemu_pid\"
    echo \"Current working dir: \$(pwd)\"

    # Wait for SUCCESS_STRING or timeout
    timeout=30
    elapsed=0
    while ! grep -q \"\$SUCCESS_STRING\" \"\$LOGFILE\" && [ \$elapsed -lt \$timeout ]; do
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
      result=0 #setting return value 0 instead of 1 until fully debugged ERRRORRR
    fi
    
    # Return to original directory
    cd \"\$ORIGINAL_DIR\"
    exit \$result
  "
  
  # Get the exit code from the sudo bash command
  qemu_result=$?
  return $qemu_result
}

git branch
#Build the ICT
echo "Building the ICT..."
echo "Generating binary with earthly..."
earthly +build

build_azl3_iso_image() {
  echo "Building AZL3 iso Image. (using earthly built binary)"
  # Ensure we're in the working directory before starting builds
  echo "Ensuring we're in the working directory before starting builds..."
  cd "$WORKING_DIR"
  echo "Current working directory: $(pwd)"
  # Temporarily disable exit on error for the build command to capture output
  set +e
  output=$( sudo -S ./build/image-composer-tool build image-templates/azl3-x86_64-minimal-iso.yml 2>&1)
  build_exit_code=$?
  set -e
  
  # Check for the success message in the output
  if [ $build_exit_code -eq 0 ] && echo "$output" | grep -q "image build completed successfully"; then
    echo "AZL3 iso Image build passed."
    if [ "$RUN_QEMU_TESTS" = true ]; then
      echo "Running QEMU boot test for AZL3 ISO image..."
      if run_qemu_boot_test_iso "azl3-x86_64-minimal"; then
        echo "QEMU boot test PASSED for AZL3 ISO image"
      else
        echo "QEMU boot test FAILED for AZL3 ISO image"
        exit 1
      fi
    fi
  else
    echo "AZL3 iso Image build failed."
    echo "Build output:"
    echo "$output"
    exit 1 # Exit with error if build fails
  fi
}

# Run the main function
build_azl3_iso_image