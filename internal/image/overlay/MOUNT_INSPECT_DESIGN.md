# Inspector.WithMountedLayout - Overlay Mount/Inspect Stage Design

**Feature ID**: #728 Mount & Inspect  
**Package**: `internal/image/overlay`  
**Type**: Pre-build baseline validation and mounting stage  
**Status**: Fully implemented and integrated  

---

## Overview

`Inspector.WithMountedLayout` is the foundational mounting stage that prepares an attached baseline loop device for chroot overlay operations. It runs **before** any packages are installed or filesystem modifications occur (Features #733-738), validating and mounting the baseline filesystem layout.

### Purpose
```
User Input:
  "Build overlay with package X on baseline Y"
         ↓
FEATURE #728 (Inspector.WithMountedLayout):
  - Validate baseline is suitable for overlay building
  - Detect & reject unsupported layouts early
  - Mount root RW and ESP read-only
  - Prepare chroot environment
         ↓
IF VALID: Proceed to build (Features #733-738)
IF INVALID: Return actionable error (no partial state)
```

---

## Architecture

### Core Components

#### 1. **Inspector struct** (lines 112-116)
```go
type Inspector struct {
    // mountBase is the workspace directory under which root/ESP are mounted.
    mountBase string
}

// Create inspector with workspace root
func NewInspector(workDir string) *Inspector {
    return &Inspector{mountBase: filepath.Join(workDir, "mnt")}
}
```

#### 2. **Layout struct** (lines 74-91)
Describes the detected filesystem layout on the mounted baseline:
```go
type Layout struct {
    PartitionTable string  // "gpt" or "dos"
    RootDevice     string  // e.g., "/dev/loop0p2"
    RootFSType     string  // "ext4", "xfs", etc.
    RootMount      string  // e.g., "/mnt/root"
    ESPDevice      string  // Empty if no ESP
    ESPMount       string  // e.g., "/mnt/root/boot/efi"
    MountBase      string  // Workspace root
}
```

#### 3. **Error Handling** (lines 97-109)
Actionable, user-facing errors with remediation:
```go
type unsupportedLayoutError struct {
    detected    string  // What was found
    reason      string  // Why unsupported
    remediation string  // How to fix
}

// Example error:
// "unsupported baseline layout: detected encrypted partition /dev/loop0p2 
// (filesystem type crypto_LUKS); overlay mode cannot modify a LUKS-encrypted 
// filesystem in place; remediation: provide an unencrypted baseline image, 
// or unlock and re-encrypt the volume out of band"
```

---

## Core Methods

### WithMountedLayout - Closure-Scoped API

**Signature**:
```go
func (insp *Inspector) WithMountedLayout(loopDevPath string, fn func(*Layout) error) error
```

**Behavior**:
1. Mount the baseline filesystem layout from `loopDevPath`
2. Invoke callback `fn` with the mounted `Layout`
3. Tear down mounts in reverse order (guaranteed on all exit paths)
4. Return callback error or mount/unmount error

**Error Handling**:
- Errors are not masked; callback failures are wrapped with context
- Teardown runs on all paths (success, failure, panic)
- Teardown errors are surfaced only when callback didn't already fail

**Usage** (from integration tests):
```go
insp := NewInspector(workDir)
err = insp.WithMountedLayout(loopDev, func(layout *Layout) error {
    // Root and ESP are mounted; root is RW, ESP is read-only
    // Perform overlay operations: install packages, generate SBOM, etc.
    return nil  // or return error to trigger rollback
})
```

### MountLayout - Explicit Lifecycle API

**Signature**:
```go
func (insp *Inspector) MountLayout(loopDevPath string) (*Layout, func() error, error)
```

**Returns**:
- `*Layout`: Mounted layout (nil on error)
- `func() error`: Idempotent teardown closure (nil on error)
- `error`: Mount/detection error

**Behavior**:
- Caller owns the teardown closure and MUST invoke it exactly once
- Typical pattern: `defer teardown()`
- On error, teardown is nil and any partial mounts are already rolled back

**Use Case**: Multi-phase builds (preprocess → build → postprocess) where the baseline must remain mounted across phase boundaries

**Usage** (from Builder/Provider pattern):
```go
layout, teardown, err := insp.MountLayout(loopDev)
if err != nil {
    return err  // No cleanup needed
}
defer teardown()  // Guaranteed unmount

// Mounts live across phases:
if err := preprocessPhase(layout); err != nil {
    return err
}
if err := buildPhase(layout); err != nil {
    return err
}
if err := postprocessPhase(layout); err != nil {
    return err
}
```

---

## Partition Layout Detection

### 1. Partition Table Detection (detectPartitionTable)

**Function**: `detectPartitionTable(loopDevPath string) (string, error)`

**Detection Order**:
1. Try `lsblk -dno PTTYPE <device>` → Parse as "gpt" or "dos"
2. Fall back to `blkid -p -s PTTYPE -o value <device>` if lsblk fails
3. Normalize to lowercase for matching

**Supported Tables**:
- `"gpt"` - GUID Partition Table (modern, recommended)
- `"dos"` - MBR/Legacy partition table

**Rejection Criteria**:
- No partition table detected: Reject with remediation to provide a partitioned image
- Unknown table type: Reject with supported types listed

**Error Format**:
```
unsupported baseline layout: detected no partition table on /dev/loop0; 
overlay mode requires a partitioned baseline image (GPT or MBR); 
remediation: provide a baseline RAW image containing a GPT or MBR partition table
```

### 2. Partition Probing (probePartitions)

**Function**: `probePartitions(loopDevPath string) ([]partition, error)`

**Process**:
1. Wait for udev to settle (timeout: 10s, best-effort)
2. Run `lsblk --json` to enumerate partitions with:
   - `NAME, PATH, FSTYPE, PARTTYPE, LABEL, PARTLABEL, SIZE, TYPE`
3. Filter to type="part" entries
4. For each partition with empty FSTYPE or PARTTYPE:
   - Direct probe via `blkid -p -s TYPE/PART_ENTRY_TYPE -o value`
   - Fills gaps left by lsblk (races with udev population)

**Result**: List of partitions with normalized (lowercase) filesystem and partition types

---

### 3. Layout Analysis (analyzeLayout)

**Function**: `analyzeLayout(table string, parts []partition) (*Layout, error)`

**Pure Analysis** - No I/O, Fully Unit-Testable

#### Rejection Phase

Early rejection prevents attempting mounts on incompatible images:

**Reject: LUKS-Encrypted Root**
- Detection: `partition.FSType == "crypto_LUKS"`
- Error message includes path and remediation
- Reason: Overlay cannot read encrypted data in place

**Reject: LVM-Backed Root**
- Detection: `partition.FSType == "lvm2_member"`
- Error message: Explains grow-only resize cannot handle LVM layers
- Reason: pvresize + lvextend required (not supported)

**Reject: dm-verity Protected Root**
- Detection: 
  - GPT: `partition.PartType` matches any verity type GUID
  - MBR: Detected via blkid label/fstype
- Error message: Explains verity hash invalidation
- Reason: Changes invalidate hash tree

**Reject: No Partitions**
- Detection: `len(parts) == 0`
- Error: Remediation to verify partitioned Linux root filesystem

#### ESP Identification

**Priority Order**:
1. GPT: Partition type GUID = `c12a7328-f81f-11d2-ba4b-00a0c93ec93b`
2. MBR: Type byte = `0xef`
3. MBR fallback: First vfat partition (only on dos/MBR, not GPT)
   - **Why MBR only**: GPT always tags ESP with GUID, so on GPT a stray vfat (e.g., `/boot` OEM partition) must not be misclassified

**Result**: `layout.ESPDevice` (empty if no ESP)

#### Root Partition Selection

**Fallback Hierarchy**:
1. **Discoverable Partitions GUID**: If partition type GUID is in `linuxRootTypeGUIDs`, it's the root
2. **Label-Based**: If partition label is "root", it's the root
3. **Largest Filesystem**: Among remaining candidates, largest FS-carrying partition wins
   - **Exclude**: Swap partitions (never root candidates), no-FS partitions

**Result**: `layout.RootDevice`, `layout.RootFSType`

#### Filesystem Support Check

**Supported Filesystems**:
- `ext4`, `ext3`, `ext2` (ext family)
- `xfs`

**Unsupported**:
- `btrfs` (not yet supported)
- `vfat` (not a Linux root FS)
- `swap` (transient storage)
- Any other type

**Error Format**:
```
unsupported baseline layout: detected root partition /dev/loop0p2 has 
filesystem type "btrfs"; overlay mode supports only ext4/ext3/ext2 and xfs 
root filesystems; remediation: use a baseline image with an ext4 or xfs 
root filesystem
```

---

## Mounting Phase

### Root Filesystem Mount (RW)

**Command**:
```bash
mount -t <fstype> <device> <mountpoint>
```

**Example**:
```bash
mount -t ext4 /dev/loop0p2 /mnt/root
```

**Mount Flags**: Write-enabled (no `-o ro`) so overlay stages can install packages

**Failure**: If mount fails, return error with device/type/mountpoint context; no partial state

### ESP Mount (Read-Only)

**Mounted Under Root** (for chroot safety):
- Path: `<rootMount>/boot/efi` (conventional EFI path inside root)
- Mount flags: `-o ro` (read-only to protect bootloader)
- Command:
  ```bash
  mount -o ro /dev/loop0p1 /mnt/root/boot/efi
  ```

**Why Read-Only**:
- Overlay stages must not mutate bootloader (Feature #738: Hardening)
- EFI/Secure Boot policy enforcement

**Nesting**:
- ESP is nested under RW root so chroot operations see it at its conventional path
- Makes `/boot/efi` accessible from chroot jail

**Failure**: If ESP mount fails:
1. Roll back (unmount) root mount
2. Return error with ESP device/type/mountpoint context
3. Guarantee no partial state

### Unmount Teardown

**Order**: Reverse mount order
1. Unmount ESP first (if present)
2. Unmount root
3. Return first error encountered (or nil on full success)

**Unmount Strategies** (from `internal/utils/mount`):
- Standard: `umount <path>`
- Lazy: `umount -l <path>` (detach immediately, cleanup on idle)
- Force: `umount -f <path>` (NFS force)
- Lazy-force: `umount -lf <path>` (most aggressive)

**Idempotency**:
- Teardown tracks mounted points internally
- Second call to teardown unmounts nothing (safe to call twice)

---

## Integration Points

### 1. Shell Allowlist (internal/utils/shell)

All commands route through allowlist to prevent injection:
```go
"lsblk", "blkid", "mount", "umount", "udevadm", "mkfs"
```

All device paths and mount points are quoted via `shell.QuoteArg()`:
```go
shell.ExecCmd("lsblk -b --json "+shell.QuoteArg(loopDevPath), ...)
```

### 2. Mount Utilities (internal/utils/mount)

Reuses existing retry/error-handling infrastructure:
- `MountPath(target, point, flags)` - Mount with block-device retry
- `UmountPath(point)` - Unmount with escalating strategies
- `IsMountPathExist(point)` - Check if mounted

### 3. Provider Integration (session.go)

The `Builder` pattern uses `MountLayout` (explicit lifecycle):
```
Provider.Preprocess():
  → layout, teardown := inspector.MountLayout(loopDev)
  → defer teardown()
  → Configure mounts, environment
     ↓
Provider.Build():
  → Layout remains mounted
  → Install packages via chroot
     ↓
Provider.Postprocess():
  → Layout still mounted
  → Generate SBOM, finalize
     ↓
Teardown triggers when builder exits
```

---

## Test Coverage

### Unit Tests (layout_test.go)

**analyzeLayout** pure-function tests (no I/O):

| Test | Scenario | Validates |
|------|----------|-----------|
| `TestAnalyzeLayout_GPTRootAndESP` | GPT with root + ESP | Both identified correctly |
| `TestAnalyzeLayout_XFSRootSupported` | XFS root | Supported FS detection |
| `TestAnalyzeLayout_MBRWithVfatESPFallback` | MBR + vfat fallback ESP | MBR-specific ESP logic |
| `TestAnalyzeLayout_PicksLargestFilesystemAsRoot` | Multi-FS, size-based selection | Largest FS wins over label |
| `TestAnalyzeLayout_SwapNotChosenAsRoot` | Swap + ext4 root | Swap correctly excluded |
| `TestAnalyzeLayout_RejectsLUKS` | LUKS partition detected | Proper error with remediation |
| `TestAnalyzeLayout_RejectsDMVerity` | dm-verity partition detected | Proper error with remediation |
| `TestAnalyzeLayout_RejectsUnsupportedFS` | btrfs root FS | Proper error with supported list |
| `TestAnalyzeLayout_RejectsLVM` | LVM2 physical volume | Proper error explaining why |
| `TestAnalyzeLayout_NoPartitions` | Empty partition list | Proper error for partitioned image |

### Integration Tests (layout_integration_test.go)

**Requires**: Root, loop devices, mkfs/mount tooling

| Test | Scenario | Validates |
|------|----------|-----------|
| `TestWithMountedLayout_RealGPTImage` | Build & mount real GPT image | Full lifecycle: detect, mount, unmount |
| `TestWithMountedLayout_RejectsRealLUKS` | Create real LUKS partition | Rejection before mount attempt |
| `TestWithMountedLayout_RejectsRealDMVerity` | Build dm-verity image | Rejection before mount attempt |
| `TestWithMountedLayout_UnmountOnExit` | Successful mount + cleanup | Proper unmount order, idempotency |
| `TestWithMountedLayout_RollbackOnFailure` | Simulate callback error | Root unmounted on callback failure |

### Provider Integration Tests

- `install_integration_test.go`: Mount baseline, install packages
- `grubupdate_integration_test.go`: Mount, detect bootloader, update config
- `session_integration_test.go`: Full multi-phase mount lifecycle

---

## Error Scenarios

### Scenario 1: Missing Partition Table

**Input**: RAW image with no partition table (e.g., raw ext4 filesystem)

**Detection**: `detectPartitionTable()` finds no table

**Error**:
```
unsupported baseline layout: detected no partition table on /dev/loop0;
overlay mode requires a partitioned baseline image (GPT or MBR);
remediation: provide a baseline RAW image containing a GPT or MBR partition table
```

### Scenario 2: LUKS-Encrypted Root

**Input**: GPT image with LUKS partition as root

**Detection**: `analyzeLayout()` sees `fstype="crypto_LUKS"`

**Error**:
```
unsupported baseline layout: detected encrypted partition /dev/loop0p2
(filesystem type crypto_LUKS); overlay mode cannot modify a LUKS-encrypted
filesystem in place; remediation: provide an unencrypted baseline image, or
unlock and re-encrypt the volume out of band
```

### Scenario 3: dm-verity Protected Root

**Input**: GPT image with dm-verity type GUID

**Detection**: `analyzeLayout()` matches verity GUID

**Error**:
```
unsupported baseline layout: detected dm-verity partition /dev/loop0p3 (type guid);
overlay mode cannot modify a dm-verity protected root because changes would
invalidate the verity hash tree; remediation: provide a baseline image without
dm-verity, or rebuild the verity tree out of band after the overlay
```

### Scenario 4: Unsupported Root Filesystem

**Input**: GPT image with btrfs root

**Detection**: `analyzeLayout()` checks supportedRootFilesystems map

**Error**:
```
unsupported baseline layout: detected root partition /dev/loop0p2 has
filesystem type "btrfs"; overlay mode supports only ext4/ext3/ext2 and xfs
root filesystems; remediation: use a baseline image with an ext4 or xfs
root filesystem
```

### Scenario 5: Mount Fails (Corrupted Filesystem)

**Input**: Valid layout but corrupted ext4 root

**Mount Phase**: `MountPath()` fails with kernel error

**Error**:
```
failed to mount root filesystem /dev/loop0p2 (ext4) at /mnt/root: 
mount: mount point does not exist / filesystem appears corrupted
```

**State**: No partial mounts (root mount failed before ESP attempt)

### Scenario 6: Unmount Fails (Busy)

**Input**: Mounted root, subprocess still has files open

**Teardown Phase**: `UmountPath()` tries standard, lazy, force strategies

**Error**:
```
failed to unmount /mnt/root: umount: target is busy / 
umount -l /mnt/root (lazy unmount succeeded)
```

**Result**: Lazy unmount succeeds; teardown returns nil or lazy error only if all strategies failed

---

## Design Principles

### 1. **Fail Fast on Invalid Layouts**
- Reject LUKS, dm-verity, LVM, unsupported FS before mounting
- No wasted mount operations on doomed images
- Actionable error messages guide user

### 2. **Reverse Cleanup Guarantee**
- Mounts recorded in order: [root, ESP]
- Teardown unmounts in reverse: [ESP, root]
- Idempotent teardown safe to call multiple times
- Guaranteed cleanup on all paths (success, error, panic)

### 3. **Read-Only ESP Protection**
- ESP mounted with `-o ro` under root
- Prevents overlay stages from corrupting bootloader
- Conventional `/boot/efi` path visible from chroot jail

### 4. **Shell Allowlist Security**
- All commands routed through `shell.ExecCmd()` with allowlist
- All paths quoted via `shell.QuoteArg()`
- No dynamic command construction or token interpolation
- Defends against command injection from user-supplied baselines

### 5. **Pure Layout Analysis**
- `analyzeLayout()` has no I/O, fully unit-testable
- Partition detection separate from mounting
- Enables mock/fuzz testing of layout logic

### 6. **Two-API Pattern**
- **WithMountedLayout**: Closure-scoped, simple one-shot use cases
- **MountLayout**: Explicit lifecycle, multi-phase providers
- Caller chooses API based on lifecycle needs

---

## Usage Examples

### Example 1: One-Shot Overlay Build

```go
insp := overlay.NewInspector(workDir)
err := insp.WithMountedLayout(loopDev, func(layout *overlay.Layout) error {
    // layout.RootMount = "/mnt/root" (mounted RW)
    // layout.ESPMount = "/mnt/root/boot/efi" (mounted read-only)
    // layout.RootFSType = "ext4" or "xfs"
    
    // Install packages via chroot
    if err := installPackages(layout); err != nil {
        return err  // Triggers unmount
    }
    
    // Generate SBOM
    if err := generateSBOM(layout); err != nil {
        return err  // Triggers unmount
    }
    
    return nil  // Triggers unmount on success
})
```

### Example 2: Multi-Phase Provider

```go
type OverlayProvider struct {
    inspector *overlay.Inspector
    layout    *overlay.Layout
    teardown  func() error
}

func (p *OverlayProvider) Preprocess(loopDev string) error {
    var err error
    p.layout, p.teardown, err = p.inspector.MountLayout(loopDev)
    if err != nil {
        return err
    }
    // Mounts live across phase boundaries
    return p.configureEnvironment()
}

func (p *OverlayProvider) Build() error {
    return p.installPackages()  // layout still mounted
}

func (p *OverlayProvider) Postprocess() error {
    defer p.teardown()  // Unmount on exit
    return p.generateSBOM()
}
```

### Example 3: Error Handling

```go
layout, teardown, err := insp.MountLayout(loopDev)
if err != nil {
    // Error is actionable: lists what was detected and why
    // Caller knows exactly what to fix
    log.Fatalf("Baseline validation failed: %v", err)
    // No cleanup needed; any partial mounts already rolled back
}
defer teardown()

// Proceed knowing baseline is valid, mounted, and will be cleaned up
```

---

## Future Enhancements

1. **Btrfs Support**: Implement `btrfs filesystem resize` for Feature #756
2. **LVM Logical Volume Detection**: Support LVM-backed roots (requires pvresize logic)
3. **Encryption-Aware Mounts**: Support pre-unlocked LUKS containers
4. **ZFS/NVMe**: Extend supported root filesystems
5. **Mount Namespace Isolation**: Consider `unshare(2)` for rootless operation

---

## References

- **Feature Issues**: #728 (Mount & Inspect), #733-738 (Build phases), #756 (Resize)
- **Shell Allowlist**: `internal/utils/shell` - Command injection prevention
- **Mount Utilities**: `internal/utils/mount` - Retry logic and unmount strategies
- **Partition Detection**: Uses `lsblk` (JSON) + `blkid` (fallback)
- **Systemd Discoverable Partitions Spec**: Linux root type GUIDs for GPT
- **Test Files**: `layout_test.go` (unit), `layout_integration_test.go` (integration)
