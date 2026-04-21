# Configure Secure Boot

This guide walks you through setting up Secure Boot for your operating system
images using the ICT tool. Follow each step carefully.

## Prerequisites

- Linux environment with OpenSSL installed
- QEMU with OVMF UEFI firmware
- ICT tool configured

## Step 1: Generate Secure Boot Keys

Create a directory for your keys and generate the required certificates:

```bash
# Create a directory for secure boot keys
mkdir -p /data/secureboot/keys
cd /data/secureboot/keys

# Generate private key and certificate using RSA 3072-bit with SHA-384
openssl req -new -x509 -newkey rsa:3072 -sha384 -keyout DB.key -out DB.crt -days 3650 -nodes -subj "/CN=ICT Secure Boot Key/"

# Convert certificate to DER format (required by UEFI)
openssl x509 -outform DER -in DB.crt -out DB.cer
```

NOTE: The signing keypair strength should align with the crypto implementation
supported by the UEFI Secure boot implementation on a specific system. The
recommendation is to test the support for `RSA3072SHA384` before moving to
`RSA2048SHA256`.

**What you'll have:**

- `DB.key` - Private key (keep secure)
- `DB.crt` - Certificate in PEM format
- `DB.cer` - Certificate in DER format (for UEFI)

## Step 2: Configure Your Template

Edit your ICT template YAML file to include the Secure Boot
configuration:

```yaml
# Add this section to your template
immutability:
  enabled: true
  secureBootDBKey: "/data/secureboot/keys/DB.key"
  secureBootDBCrt: "/data/secureboot/keys/DB.crt"
  secureBootDBCer: "/data/secureboot/keys/DB.cer"
```

**Important:** Use absolute paths to your key files.

## Step 3: Build Your OS Image

Run ICT to build your image as usual.

## Step 4: Verify Build Output

After a successful build, check the output directory, for example:

```bash
ls /tmp/image-composer-tool/wind-river-elxr-elxr12-x86_64/imagebuild/Default_Raw/ -la
```

**Expected output:**

- `minimal-os-image-elxr.raw` - Your bootable OS image
- `DB.cer` - Secure Boot certificate (copied during build)

## Step 5: Prepare Image for Testing

Copy the certificate to the EFI partition for easier key enrollment:

```bash
# Mount the raw image
sudo losetup -Pf minimal-os-image-elxr.raw

# Find the loop device (usually /dev/loop0)
LOOP_DEVICE=$(losetup -l | grep minimal-os-image-elxr.raw | awk '{print $1}')
echo "Using loop device: $LOOP_DEVICE"

# Check partitions
lsblk $LOOP_DEVICE

# Mount EFI partition (usually partition 1)
sudo mkdir -p /mnt/efi
sudo mount ${LOOP_DEVICE}p1 /mnt/efi

# Create keys directory and copy certificate
sudo mkdir -p /mnt/efi/EFI/keys
sudo cp DB.cer /mnt/efi/EFI/keys/

# Cleanup
sudo umount /mnt/efi
sudo losetup -d $LOOP_DEVICE
```

## Step 6: Boot Image in QEMU

Launch QEMU with UEFI firmware:

```bash
sudo qemu-system-x86_64 \
  -m 2048 \
  -enable-kvm \
  -cpu host \
  -bios /usr/share/OVMF/OVMF_CODE.fd \
  -device virtio-scsi-pci \
  -drive if=none,id=drive0,file=minimal-os-image-elxr.raw,format=raw \
  -device scsi-hd,drive=drive0 \
  -nographic \
  -serial mon:stdio \
  -boot menu=on
```

**Tip:** Press `Esc` repeatedly as soon as QEMU starts to enter UEFI setup.

## Step 7: Enroll Secure Boot Keys

Once you're in the UEFI setup menu, do the following.

**Note:** Menu names vary by firmware. Look for similar options if the exact
names differ.

### Navigate to Secure Boot

1. Use arrow keys to find **"Device Manager"** or
   **"Secure Boot Configuration"**
2. Look for **"Secure Boot"** or **"Security"** menu

### Enable Custom Mode

1. Find **"Secure Boot Mode"**
2. Change from **"Standard"** to **"Custom"**
3. This allows manual key management

### Enroll Your Key

1. Navigate to **"Custom Secure Boot Options"**
2. Select **"DB Options"** (Database Options)
3. Choose **"Enroll Signature"** or **"Enroll DB"**
4. Navigate to: **`fs0:\EFI\keys\DB.cer`**
5. Select the file and confirm enrollment

### Save and Exit

1. Press **F10** to save changes
2. Select **"Reset"** or **"Exit"**
3. System will reboot

## Step 8: Verify Secure Boot

After the system boots completely, verify that Secure Boot is working:

```bash
# Check if Secure Boot is enabled
sudo dmesg | grep -i secure

# Expected output:
# [    0.000000] secureboot: Secure boot enabled
# [    0.716009] integrity: Loaded X.509 cert 'ICT Secure Boot Key: [key-hash]'
```

## Troubleshooting

**Common Issues:**

1. **Can't find keys in UEFI:** Ensure the EFI partition is mounted and files
   are in `/EFI/keys/`.
2. **Secure Boot not enabled:** Verify you're in "Custom" mode, not
   "Standard" mode.
3. **Boot fails after key enrollment:** Check that your image was built with
   the same keys.

**Recovery:**

- Boot QEMU without Secure Boot: Remove `-bios /usr/share/OVMF/OVMF_CODE.fd`
- Reset UEFI settings: In UEFI setup, look for "Reset to defaults."

## Summary

You've successfully:

- ✅ Generated Secure Boot keys
- ✅ Built an image with Secure Boot enabled
- ✅ Enrolled keys in UEFI firmware
- ✅ Verified Secure Boot functionality
