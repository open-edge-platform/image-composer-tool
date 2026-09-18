// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Real merged `disk:` blocks for every buildable combination in the Basic/
// Advanced manifest (internal/api/service/data/manifest.yaml).
//
// Captured verbatim from `image-composer-tool resolve --full <template>`, which
// is the same merge POST /templates/compose runs (internal/api/service/
// compose.go: LoadAndMergeTemplate -> MarshalTemplateYAML). Using real output
// rather than hand-written YAML is the point: it keeps the parser honest about
// what the server actually sends, including the empty keys the Go structs emit
// because they carry no `omitempty`.
//
// Regenerate after a template or OS-default change:
//   go build -o build/ict ./cmd/image-composer-tool
//   ./build/ict resolve --full image-templates/<name>.yml
// then copy the `disk:` block across.

// debian13-x86_64-desktop-virtualization-iso.yml
export const debianDesktopVirtIso = `
disk:
    name: Default_ISO
    path: ""
    artifacts: []
    size: ""
    partitionTableType: gpt
    partitions:
        - name: ""
          id: boot
          flags:
            - esp
            - boot
          type: esp
          typeUUID: ""
          fsType: fat32
          fsLabel: ""
          start: 1MiB
          end: 513MiB
          mountPoint: /boot/efi
          mountOptions: ""
        - name: ""
          id: rootfs
          flags: []
          type: linux-root-amd64
          typeUUID: ""
          fsType: ext4
          fsLabel: ""
          start: 513MiB
          end: "0"
          mountPoint: /
          mountOptions: ""
`

// ubuntu24-x86_64-robotics-jazzy-iso.yml
export const roboticsJazzyIso = `
disk:
    name: Default_ISO
    path: ""
    artifacts: []
    size: ""
    partitionTableType: gpt
    partitions:
        - name: ""
          id: boot
          flags:
            - esp
            - boot
          type: esp
          typeUUID: ""
          fsType: fat32
          fsLabel: ""
          start: 1MiB
          end: 513MiB
          mountPoint: /boot/efi
          mountOptions: ""
        - name: ""
          id: rootfs
          flags: []
          type: linux-root-amd64
          typeUUID: ""
          fsType: ext4
          fsLabel: ""
          start: 513MiB
          end: "0"
          mountPoint: /
          mountOptions: ""
`

// ubuntu24-x86_64-minimal-ptl-pv-raw.yml
export const minimalPtlPvRaw = `
disk:
    name: minimal-desktop-ubuntu-ptl-pv
    path: ""
    artifacts:
        - type: raw
          compression: gz
    size: 32GiB
    partitionTableType: gpt
    partitions:
        - name: EFI
          id: EFI
          flags:
            - boot
            - esp
          type: esp
          typeUUID: c12a7328-f81f-11d2-ba4b-00a0c93ec93b
          fsType: vfat
          fsLabel: ""
          start: 1MiB
          end: 1025MiB
          mountPoint: /boot/efi
          mountOptions: defaults
        - name: SWAP
          id: SWAP
          flags: []
          type: linux-swap
          typeUUID: 0657fd6d-a4ab-43c4-84e5-0933c84b4f4f
          fsType: linux-swap
          fsLabel: ""
          start: 1025MiB
          end: 3073MiB
          mountPoint: none
          mountOptions: sw
        - name: ROOT
          id: ROOT
          flags: []
          type: linux-root-amd64
          typeUUID: 4f68bce3-e8cd-4db1-96e7-fbcaf984b709
          fsType: ext4
          fsLabel: ""
          start: 3073MiB
          end: "0"
          mountPoint: /
          mountOptions: defaults
`

// ubuntu24/generic-handheld-os-template.yml
export const genericHandheldRaw = `
disk:
    name: minimal-desktop-ubuntu
    path: ""
    artifacts:
        - type: raw
          compression: gz
    size: 32GiB
    partitionTableType: gpt
    partitions:
        - name: EFI
          id: EFI
          flags:
            - boot
            - esp
          type: esp
          typeUUID: c12a7328-f81f-11d2-ba4b-00a0c93ec93b
          fsType: vfat
          fsLabel: ""
          start: 1MiB
          end: 1025MiB
          mountPoint: /boot/efi
          mountOptions: defaults
        - name: SWAP
          id: SWAP
          flags: []
          type: linux-swap
          typeUUID: 0657fd6d-a4ab-43c4-84e5-0933c84b4f4f
          fsType: linux-swap
          fsLabel: ""
          start: 1025MiB
          end: 3073MiB
          mountPoint: none
          mountOptions: sw
        - name: ROOT
          id: ROOT
          flags: []
          type: linux-root-amd64
          typeUUID: 4f68bce3-e8cd-4db1-96e7-fbcaf984b709
          fsType: ext4
          fsLabel: ""
          start: 3073MiB
          end: "0"
          mountPoint: /
          mountOptions: defaults
`

export const ALL_FIXTURES: { name: string; yaml: string }[] = [
  { name: 'debian13-x86_64-desktop-virtualization-iso.yml', yaml: debianDesktopVirtIso },
  { name: 'ubuntu24-x86_64-robotics-jazzy-iso.yml', yaml: roboticsJazzyIso },
  { name: 'ubuntu24-x86_64-minimal-ptl-pv-raw.yml', yaml: minimalPtlPvRaw },
  { name: 'ubuntu24/generic-handheld-os-template.yml', yaml: genericHandheldRaw },
]
