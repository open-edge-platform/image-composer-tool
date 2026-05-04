#!/bin/bash
set -euo pipefail

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
  local cleanup_type="${1:-all}"
  
  case "$cleanup_type" in
    "raw")
      echo "Cleaning up raw image files from build directories..."
      sudo rm -rf ./tmp/*/imagebuild/*/*.raw 2>/dev/null || true
      sudo rm -rf ./workspace/*/imagebuild/*/*.raw 2>/dev/null || true
      ;;
    "extracted")
      echo "Cleaning up extracted image files in current directory..."
      rm -f ./*.raw 2>/dev/null || true
      ;;
    "all"|*)
      echo "Cleaning up all temporary image files..."
      sudo rm -rf ./tmp/*/imagebuild/*/*.raw 2>/dev/null || true
      sudo rm -rf ./workspace/*/imagebuild/*/*.raw 2>/dev/null || true
      rm -f ./*.raw 2>/dev/null || true
      ;;
  esac
}

run_qemu_boot_test() {
  local IMAGE_PATTERN="$1"
  if [ -z "$IMAGE_PATTERN" ]; then
    echo "Error: Image pattern not provided to run_qemu_boot_test"
    return 1
  fi
  
  local RAW_IMAGE=""
  local SUCCESS_STRING="login:"
  local LOGFILE="/tmp/qemu_boot_test_${IMAGE_PATTERN}.log"
  local ORIGINAL_DIR
  ORIGINAL_DIR="$(pwd)"
  
  echo "Looking for compressed raw image matching pattern: *${IMAGE_PATTERN}*.raw.gz"
  
  cd "$WORKING_DIR"
  
  if ls ./*"${IMAGE_PATTERN}"*.raw.gz 1>/dev/null 2>&1; then
    GZ_IMAGE=$(ls ./*"${IMAGE_PATTERN}"*.raw.gz | head -1)
    echo "Found compressed image: $GZ_IMAGE"
    echo "Extracting..."
    gunzip -k "$GZ_IMAGE"
    RAW_IMAGE="${GZ_IMAGE%.gz}"
    
    if [ ! -f "$RAW_IMAGE" ]; then
      echo "Error: extraction failed, $RAW_IMAGE not found"
      cd "$ORIGINAL_DIR"
      return 1
    fi
    
    IMAGE="$RAW_IMAGE"
  else
    echo "Compressed raw image file matching pattern '*${IMAGE_PATTERN}*.raw.gz' not found!"
    return 1
  fi

  echo "Booting image: $IMAGE"
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
        -append 'console=ttyS0' > \"\$LOGFILE\" 2>&1 &
    
    QEMU_PID=\$!
    TIMEOUT=180
    ELAPSED=0
    
    while [ \$ELAPSED -lt \$TIMEOUT ]; do
      if grep -q \"\$SUCCESS_STRING\" \"\$LOGFILE\" 2>/dev/null; then
        echo \"Boot successful - found '\$SUCCESS_STRING' in output\"
        kill \$QEMU_PID 2>/dev/null || true
        break
      fi
      if ! ps -p \$QEMU_PID > /dev/null 2>&1; then
        echo \"QEMU process exited unexpectedly\"
        break
      fi
      sleep 5
      ELAPSED=\$((ELAPSED + 5))
      echo \"Waiting for boot... (\${ELAPSED}s/\${TIMEOUT}s)\"
    done
    
    kill \$QEMU_PID 2>/dev/null || true
    
    if grep -q \"\$SUCCESS_STRING\" \"\$LOGFILE\"; then
      echo \"Boot test PASSED\"
      result=0
    else
      echo \"Boot failed or timed out\"
      result=1
    fi
    
    if [ -f \"\$RAW_IMAGE\" ]; then
      echo \"Cleaning up extracted image file: \$RAW_IMAGE\"
      rm -f \"\$RAW_IMAGE\"
    fi
    
    cd \"\$ORIGINAL_DIR\"
    exit \$result
  "
  
  qemu_result=$?
  return $qemu_result
}

git branch

echo "Building the ICT..."
echo "Generating binary with go build..."
go build ./cmd/image-composer-tool

build_azl3_dlstreamer_image() {
  echo "Building AZL3 DLStreamer Image. (using image-composer-tool binary)"
  echo "Ensuring we're in the working directory before starting builds..."
  cd "$WORKING_DIR"
  echo "Current working directory: $(pwd)"
  
  set +e
  output=$(sudo -S ./image-composer-tool build image-templates/azl3-x86_64-dlstreamer.yml 2>&1)
  build_exit_code=$?
  set -e
  
  if [ $build_exit_code -eq 0 ] && echo "$output" | grep -q "image build completed successfully"; then
    echo "AZL3 DLStreamer Image build passed."
    if [ "$RUN_QEMU_TESTS" = true ]; then
      echo "Running QEMU boot test for AZL3 DLStreamer image..."
      if run_qemu_boot_test "azl3-dlstreamer-x86_64"; then
        echo "QEMU boot test PASSED for AZL3 DLStreamer image"
      else
        echo "QEMU boot test FAILED for AZL3 DLStreamer image"
        exit 1
      fi
      cleanup_image_files raw
    fi
  else
    echo "AZL3 DLStreamer Image build failed."
    echo "Build output:"
    echo "$output"
    exit 1
  fi
}

build_azl3_dlstreamer_image
