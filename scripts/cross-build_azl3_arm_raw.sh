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

# Centralized cleanup function for image files
cleanup_image_files() {
  local cleanup_type="${1:-all}"  # Options: all, raw, extracted
  
  case "$cleanup_type" in
    "raw")
      echo "Cleaning up raw image files from build directories..."
      sudo rm -rf ./tmp/*/imagebuild/*/*.raw 2>/dev/null || true
      sudo rm -rf ./workspace/*/imagebuild/*/*.raw 2>/dev/null || true
      ;;
    "extracted")
      echo "Cleaning up extracted image files in current directory..."
      rm -f *.raw 2>/dev/null || true
      ;;
    "all"|*)
      echo "Cleaning up all temporary image files..."
      sudo rm -rf ./tmp/*/imagebuild/*/*.raw 2>/dev/null || true
      sudo rm -rf ./workspace/*/imagebuild/*/*.raw 2>/dev/null || true
      rm -f *.raw 2>/dev/null || true
      ;;
  esac
}

run_qemu_boot_test() {
  local IMAGE_PATTERN="$1"
  if [ -z "$IMAGE_PATTERN" ]; then
    echo "Error: Image pattern not provided to run_qemu_boot_test"
    return 1
  fi
  
  BIOS="/usr/share/OVMF/OVMF_CODE_4M.fd"
  TIMEOUT=30
  SUCCESS_STRING="login:"
  LOGFILE="qemu_serial.log"

  ORIGINAL_DIR=$(pwd)
  # Find compressed raw image path using pattern, handle permission issues
  FOUND_PATH=$(sudo -S find . -type f -name "*${IMAGE_PATTERN}*.raw.gz" 2>/dev/null | head -n 1)
  if [ -n "$FOUND_PATH" ]; then
    echo "Found compressed image at: $FOUND_PATH"
    IMAGE_DIR=$(dirname "$FOUND_PATH")
    
    # Fix permissions for the image directory recursively to allow access
    IMAGE_ROOT_DIR=$(echo "$IMAGE_DIR" | cut -d'/' -f2)  # Get the root directory (workspace or tmp)
    echo "Setting permissions recursively for ./$IMAGE_ROOT_DIR directory"
    sudo chmod -R 777 "./$IMAGE_ROOT_DIR"
    
    cd "$IMAGE_DIR"
    
    # Extract the .raw.gz file
    COMPRESSED_IMAGE=$(basename "$FOUND_PATH")
    RAW_IMAGE="${COMPRESSED_IMAGE%.gz}"
    echo "Extracting $COMPRESSED_IMAGE to $RAW_IMAGE..."
    
    # Check available disk space before extraction
    AVAILABLE_SPACE=$(df . | tail -1 | awk '{print $4}')
    COMPRESSED_SIZE=$(stat -c%s "$COMPRESSED_IMAGE" 2>/dev/null || echo "0")
    # Estimate uncompressed size (typically 4-6x larger for these images, being conservative)
    ESTIMATED_SIZE=$((COMPRESSED_SIZE * 6 / 1024))
    
    echo "Disk space check: Available=${AVAILABLE_SPACE}KB, Estimated needed=${ESTIMATED_SIZE}KB"
    
    # Always try aggressive cleanup first to ensure maximum space
    echo "Performing aggressive cleanup before extraction..."
    sudo rm -f *.raw 2>/dev/null || true
    sudo rm -f /tmp/*.raw 2>/dev/null || true
    sudo rm -rf ../../../cache/ 2>/dev/null || true
    sudo rm -rf ../../../tmp/*/imagebuild/*/*.raw 2>/dev/null || true
    sudo rm -rf ../../../workspace/*/imagebuild/*/*.raw 2>/dev/null || true
    
    # Force filesystem sync and check space again
    sync
    AVAILABLE_SPACE=$(df . | tail -1 | awk '{print $4}')
    echo "Available space after cleanup: ${AVAILABLE_SPACE}KB"
    
    if [ "$AVAILABLE_SPACE" -lt "$ESTIMATED_SIZE" ]; then
      echo "Warning: Still insufficient disk space after cleanup"
      echo "Attempting extraction to /tmp with streaming..."
      
      # Check /tmp space
      TMP_AVAILABLE=$(df /tmp | tail -1 | awk '{print $4}')
      echo "/tmp available space: ${TMP_AVAILABLE}KB"
      
      if [ "$TMP_AVAILABLE" -gt "$ESTIMATED_SIZE" ]; then
        TMP_RAW="/tmp/$RAW_IMAGE"
        echo "Extracting to /tmp first..."
        if gunzip -c "$COMPRESSED_IMAGE" > "$TMP_RAW"; then
          echo "Successfully extracted to /tmp, moving to final location..."
          if mv "$TMP_RAW" "$RAW_IMAGE"; then
            echo "Successfully moved extracted image to current directory"
          else
            echo "Failed to move from /tmp, will try to use /tmp location directly"
            ln -sf "$TMP_RAW" "$RAW_IMAGE" 2>/dev/null || cp "$TMP_RAW" "$RAW_IMAGE"
          fi
        else
          echo "Failed to extract to /tmp"
          rm -f "$TMP_RAW" 2>/dev/null || true
          return 1
        fi
      else
        echo "ERROR: Insufficient space in both current directory and /tmp"
        echo "Current: ${AVAILABLE_SPACE}KB, /tmp: ${TMP_AVAILABLE}KB, Needed: ${ESTIMATED_SIZE}KB"
        return 1
      fi
    else
      echo "Sufficient space available, extracting directly..."
      if ! gunzip -c "$COMPRESSED_IMAGE" > "$RAW_IMAGE"; then
        echo "Direct extraction failed, cleaning up partial file..."
        rm -f "$RAW_IMAGE" 2>/dev/null || true
        return 1
      fi
    fi
    
    if [ ! -f "$RAW_IMAGE" ]; then
      echo "Failed to extract image!"
      # Clean up any partially extracted files
      sudo rm -f "$RAW_IMAGE" /tmp/"$RAW_IMAGE" 2>/dev/null || true
      cd "$ORIGINAL_DIR"
      return 1
    fi
    
    IMAGE="$RAW_IMAGE"
  else
    echo "Compressed raw image file matching pattern '*${IMAGE_PATTERN}*.raw.gz' not found!"
    return 1
  fi

  
  echo "Booting image: $IMAGE "
  #create log file ,boot image into qemu , return the pass or fail after boot sucess
  sudo bash -c "
    LOGFILE=\"$LOGFILE\"
    SUCCESS_STRING=\"$SUCCESS_STRING\"
    IMAGE=\"$IMAGE\"
    RAW_IMAGE=\"$RAW_IMAGE\"
    ORIGINAL_DIR=\"$ORIGINAL_DIR\"
        #-enable-kvm \\
    
    touch \"\$LOGFILE\" && chmod 666 \"\$LOGFILE\"    
    nohup qemu-system-aarch64 \\
        -m 2048 \\
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
      result=1
    fi
    
    # Clean up extracted raw file
    if [ -f \"\$RAW_IMAGE\" ]; then
      echo \"Cleaning up extracted image file: \$RAW_IMAGE\"
      rm -f \"\$RAW_IMAGE\"
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
echo "Generating binary with go build..."
go build ./cmd/image-composer-tool

build_azl3_raw_image() {
  echo "Building AZL3 raw Image for ARM64. (using image-composer-tool binary)"
  # Ensure we're in the working directory before starting builds
  echo "Ensuring we're in the working directory before starting builds..."
  cd "$WORKING_DIR"
  echo "Current working directory: $(pwd)"
  
 # Temporarily disable exit on error for the build command to capture output
  set +e
  output=$( sudo -S ./image-composer-tool --verbose build image-templates/azl3-aarch64-edge-raw.yml 2>&1)
  build_exit_code=$?
  set -e

  # Check for the success message in the output
  if [ $build_exit_code -eq 0 ] && echo "$output" | grep -q "image build completed successfully"; then
    echo "AZL3 raw Image build passed."
    if [ "$RUN_QEMU_TESTS" = true ]; then
      echo "Running QEMU boot test for AZL3 raw image..."
      if run_qemu_boot_test "azl3-aarch64-edge"; then
        echo "QEMU boot test PASSED for AZL3 raw image"
      else
        echo "QEMU boot test FAILED for AZL3 raw image"
        exit 1
      fi
      # Clean up after QEMU test to free space
      cleanup_image_files raw
    fi
  else
    echo "AZL3 raw Image build failed."
    echo "Build output:"
    echo "$output"
    exit 1 # Exit with error if build fails
  fi
}

# Run the main function
build_azl3_raw_image
