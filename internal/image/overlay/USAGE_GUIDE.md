# Inspector.WithMountedLayout - Practical Usage Guide

**Quick Reference**: How to use the mount/inspect stage in your overlay builder  
**Audience**: Overlay provider developers, integration developers  
**Version**: Feature #728, Production Ready  

---

## Quick Start: One-Shot Pattern

The simplest way to mount and work with a baseline image:

```go
package myprovider

import (
	"fmt"
	"path/filepath"
	"github.com/open-edge-platform/image-composer-tool/internal/image/overlay"
)

func BuildOverlay(loopDev, workDir string) error {
	insp := overlay.NewInspector(workDir)
	
	err := insp.WithMountedLayout(loopDev, func(layout *overlay.Layout) error {
		fmt.Printf("Root device: %s\n", layout.RootDevice)        // e.g., /dev/loop0p2
		fmt.Printf("Root mount: %s\n", layout.RootMount)          // e.g., /mnt/root
		fmt.Printf("Root filesystem: %s\n", layout.RootFSType)    // e.g., ext4
		fmt.Printf("ESP device: %s\n", layout.ESPDevice)          // e.g., /dev/loop0p1
		fmt.Printf("ESP mount: %s\n", layout.ESPMount)            // e.g., /mnt/root/boot/efi
		fmt.Printf("Partition table: %s\n", layout.PartitionTable) // e.g., gpt
		
		// Root is mounted RW at layout.RootMount
		// ESP (if present) is mounted read-only at layout.ESPMount
		
		// Install packages, generate SBOM, etc.
		if err := installPackages(layout); err != nil {
			return err  // Triggers automatic unmount
		}
		
		if err := generateSBOM(layout); err != nil {
			return err  // Triggers automatic unmount
		}
		
		return nil  // Triggers automatic unmount on success
	})
	
	if err != nil {
		return fmt.Errorf("overlay build failed: %w", err)
	}
	
	return nil
}

func installPackages(layout *overlay.Layout) error {
	// Use layout.RootMount as chroot base
	// layout.RootMount/boot/efi is read-only bootloader
	return nil
}

func generateSBOM(layout *overlay.Layout) error {
	// SBOM tools can read from layout.RootMount
	return nil
}
```

---

## Multi-Phase Pattern: Builder/Provider

When you need mounts to span multiple phases (preprocess → build → postprocess):

```go
package myprovider

import (
	"fmt"
	"github.com/open-edge-platform/image-composer-tool/internal/image/overlay"
)

type MyProvider struct {
	workDir    string
	inspector  *overlay.Inspector
	layout     *overlay.Layout
	teardown   func() error
}

func NewMyProvider(workDir string) *MyProvider {
	return &MyProvider{
		workDir:   workDir,
		inspector: overlay.NewInspector(workDir),
	}
}

// Preprocess: Detect and mount baseline
func (p *MyProvider) Preprocess(loopDev string) error {
	var err error
	p.layout, p.teardown, err = p.inspector.MountLayout(loopDev)
	if err != nil {
		return fmt.Errorf("failed to mount baseline: %w", err)
	}
	
	fmt.Printf("Baseline mounted at: %s\n", p.layout.RootMount)
	fmt.Printf("Filesystem type: %s\n", p.layout.RootFSType)
	
	// Configure environment, prepare chroot, etc.
	return p.setupChroot()
}

// Build: Install packages (layout still mounted)
func (p *MyProvider) Build() error {
	if p.layout == nil {
		return fmt.Errorf("preprocess not called")
	}
	
	return p.installPackages()
}

// Postprocess: Generate artifacts (layout still mounted)
func (p *MyProvider) Postprocess() error {
	if p.layout == nil {
		return fmt.Errorf("preprocess not called")
	}
	
	defer func() {
		// Always unmount when done
		if err := p.teardown(); err != nil {
			fmt.Printf("Warning: unmount failed: %v\n", err)
		}
	}()
	
	return p.generateArtifacts()
}

func (p *MyProvider) setupChroot() error {
	// Create chroot environment
	fmt.Printf("Setting up chroot at: %s\n", p.layout.RootMount)
	return nil
}

func (p *MyProvider) installPackages() error {
	// p.layout.RootMount is still mounted and available
	fmt.Printf("Installing packages in: %s\n", p.layout.RootMount)
	return nil
}

func (p *MyProvider) generateArtifacts() error {
	// p.layout.RootMount is still mounted
	fmt.Printf("Generating SBOM from: %s\n", p.layout.RootMount)
	return nil
}
```

---

## Chroot Integration

Accessing the mounted baseline from within a chroot:

```go
func enterChroot(layout *overlay.Layout, command []string) error {
	// layout.RootMount contains the baseline filesystem
	// layout.ESPMount (if present) is mounted at /boot/efi inside root
	
	// Example: Install package via chroot
	chrootCmd := append([]string{"chroot", layout.RootMount}, command...)
	// chrootCmd = ["chroot", "/mnt/root", "apt-get", "install", "-y", "nginx"]
	
	return executeCommand(chrootCmd)
}

// Usage:
func installNginx(layout *overlay.Layout) error {
	return enterChroot(layout, []string{"apt-get", "install", "-y", "nginx"})
}
```

---

## ESP/Bootloader Access

The ESP is mounted read-only for inspection:

```go
func inspectBootloader(layout *overlay.Layout) error {
	// ESP is mounted at layout.ESPMount (e.g., /mnt/root/boot/efi)
	// It's read-only, so bootloader cannot be accidentally modified
	
	if layout.ESPMount == "" {
		fmt.Println("No ESP found (BIOS mode)")
		return nil
	}
	
	// Read-only bootloader inspection
	fmt.Printf("ESP is at: %s\n", layout.ESPMount)
	
	// Can read from layout.ESPMount, but cannot write
	// (read-only mount enforced by kernel)
	
	return nil
}
```

---

## Error Handling: Actionable Messages

When validation fails, errors include what to fix:

```go
func BuildOverlay(loopDev, workDir string) error {
	insp := overlay.NewInspector(workDir)
	
	_, teardown, err := insp.MountLayout(loopDev)
	if err != nil {
		// Error message example:
		// "unsupported baseline layout: detected encrypted partition 
		//  /dev/loop0p2 (filesystem type crypto_LUKS); overlay mode 
		//  cannot modify a LUKS-encrypted filesystem in place; 
		//  remediation: provide an unencrypted baseline image, or 
		//  unlock and re-encrypt the volume out of band"
		
		return fmt.Errorf("cannot build overlay: %w", err)
	}
	
	// No cleanup needed here because MountLayout failed before mounting
	
	defer teardown()
	
	// ... proceed with build ...
	
	return nil
}
```

---

## Filesystem-Specific Operations

Different filesystems require different tools:

```go
func growFilesystem(layout *overlay.Layout) error {
	switch layout.RootFSType {
	case "ext4", "ext3", "ext2":
		// Use resize2fs for ext filesystems
		// resize2fs -f /dev/loop0p2
		fmt.Printf("Growing ext filesystem with resize2fs\n")
		
	case "xfs":
		// Use xfs_growfs for XFS
		// xfs_growfs /mnt/root
		fmt.Printf("Growing XFS filesystem with xfs_growfs\n")
		
	default:
		return fmt.Errorf("unsupported filesystem type: %s", layout.RootFSType)
	}
	
	return nil
}
```

---

## Testing Your Provider

Unit test pattern for providers using WithMountedLayout:

```go
package myprovider_test

import (
	"testing"
	"github.com/open-edge-platform/image-composer-tool/internal/image/overlay"
)

func TestMyProvider_WithMountedLayout(t *testing.T) {
	// Create temporary workspace
	workDir := t.TempDir()
	provider := myprovider.NewMyProvider(workDir)
	
	// Mock layout for testing (no real image needed)
	mockLayout := &overlay.Layout{
		PartitionTable: "gpt",
		RootDevice:     "/dev/loop99p2",
		RootFSType:     "ext4",
		RootMount:      "/mnt/test-root",
		ESPDevice:      "/dev/loop99p1",
		ESPMount:       "/mnt/test-root/boot/efi",
		MountBase:      workDir,
	}
	
	// Test with mock layout
	if err := provider.ProcessLayout(mockLayout); err != nil {
		t.Fatalf("ProcessLayout failed: %v", err)
	}
}

func (p *MyProvider) ProcessLayout(layout *overlay.Layout) error {
	// Your provider logic here
	return nil
}
```

---

## Real Example: From Session Integration Tests

This is how the overlay system actually uses WithMountedLayout:

```go
// From session_integration_test.go
func TestBuilder_MultiPhaseLifecycle(t *testing.T) {
	// 1. Create base image
	loopDev := attachTestImage(t)
	defer detachTestImage(t, loopDev)
	
	// 2. Create inspector
	insp := overlay.NewInspector(t.TempDir())
	
	// 3. Use WithMountedLayout for one-shot test
	err := insp.WithMountedLayout(loopDev, func(layout *overlay.Layout) error {
		// 4. Verify layout
		if layout.RootDevice == "" {
			return fmt.Errorf("no root device detected")
		}
		
		if layout.RootFSType != "ext4" {
			return fmt.Errorf("unexpected FS type: %s", layout.RootFSType)
		}
		
		// 5. Perform operations (root is mounted RW)
		if err := verifyMountedFilesystem(layout.RootMount); err != nil {
			return err
		}
		
		return nil
	})
	
	if err != nil {
		t.Fatalf("WithMountedLayout failed: %v", err)
	}
	
	// 6. Mounts automatically cleaned up here
}
```

---

## Debugging: Inspect Layout

When troubleshooting, examine the detected layout:

```go
func debugLayout(layout *overlay.Layout) {
	println("=== LAYOUT DEBUG ===")
	println("Partition Table:", layout.PartitionTable)
	println("Root Device:    ", layout.RootDevice)
	println("Root FS Type:   ", layout.RootFSType)
	println("Root Mount:     ", layout.RootMount)
	println("ESP Device:     ", layout.ESPDevice)
	println("ESP Mount:      ", layout.ESPMount)
	println("Mount Base:     ", layout.MountBase)
	println("=== END DEBUG ===")
}

// Usage in callback:
insp.WithMountedLayout(loopDev, func(layout *overlay.Layout) error {
	debugLayout(layout)
	return nil
})
```

---

## Common Patterns

### Pattern 1: Mount Once, Use Multiple Times

```go
layout, teardown, err := insp.MountLayout(loopDev)
if err != nil {
	return err
}
defer teardown()

// Use layout multiple times across functions
if err := phase1(layout); err != nil {
	return err
}
if err := phase2(layout); err != nil {
	return err
}
if err := phase3(layout); err != nil {
	return err
}
// Teardown happens once at end
```

### Pattern 2: Conditional Operations Based on Layout

```go
insp.WithMountedLayout(loopDev, func(layout *overlay.Layout) error {
	if layout.ESPDevice == "" {
		// BIOS mode, no EFI system partition
		return buildBIOSOverlay(layout)
	} else {
		// UEFI mode, has ESP
		return buildUEFIOverlay(layout)
	}
})
```

### Pattern 3: Error Propagation

```go
insp.WithMountedLayout(loopDev, func(layout *overlay.Layout) error {
	// Errors from helper functions automatically trigger unmount
	if err := helper1(layout); err != nil {
		// Unmount happens automatically
		return fmt.Errorf("helper1 failed: %w", err)
	}
	
	if err := helper2(layout); err != nil {
		// Unmount happens automatically
		return fmt.Errorf("helper2 failed: %w", err)
	}
	
	return nil
	// Unmount happens here on success
})
```

---

## Performance Considerations

### Avoid Repeated Mounts

❌ **Bad**: Mounting multiple times
```go
// DON'T DO THIS
for _, pkg := range packages {
    insp.WithMountedLayout(loopDev, func(layout *overlay.Layout) error {
        return installPackage(layout, pkg)
    })
}
```

✅ **Good**: Mount once, use many times
```go
// DO THIS
layout, teardown, err := insp.MountLayout(loopDev)
if err != nil {
    return err
}
defer teardown()

for _, pkg := range packages {
    if err := installPackage(layout, pkg); err != nil {
        return err
    }
}
```

---

## Troubleshooting

### Mount Fails: Device Not Ready

**Error**: `failed to mount /dev/loop0p2: device does not exist`

**Cause**: Loop device still initializing

**Solution**: Code handles this with retry logic (up to 5 attempts, 200ms delay)

**Check**:
```bash
lsblk /dev/loop0
# Should show partitions p1, p2
```

### Mount Fails: Already Mounted

**Error**: `mount: /mnt/root already mounted`

**Cause**: Previous process didn't clean up

**Solution**: Manually clean up
```bash
sudo umount /mnt/root/boot/efi 2>/dev/null
sudo umount /mnt/root 2>/dev/null
sudo losetup -d /dev/loop0
```

### Partition Not Detected

**Error**: `unsupported baseline layout: no partitions on the baseline image`

**Cause**: Image has no partition table (raw filesystem)

**Solution**: Create partitioned image
```bash
# Create GPT partition table
sgdisk -n 1:1MiB:16MiB -t 1:EF00 image.raw
sgdisk -n 2:16MiB:0 -t 2:8300 image.raw
```

### Filesystem Not Recognized

**Error**: `unsupported baseline layout: root has filesystem type "btrfs"`

**Cause**: Btrfs is not yet supported by overlay mode

**Solution**: Use ext4 or xfs for root filesystem

---

## See Also

- **Design Doc**: [MOUNT_INSPECT_DESIGN.md](MOUNT_INSPECT_DESIGN.md)
- **Test Matrix**: [TEST_VALIDATION_MATRIX.md](TEST_VALIDATION_MATRIX.md)
- **Feature Issue**: #728 Mount & Inspect
- **Package**: `internal/image/overlay`
- **Tests**: `layout.go`, `layout_test.go`, `layout_integration_test.go`
