package imageos

import (
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestParseCryptsetupStatusDevice(t *testing.T) {
	t.Parallel()

	status := `/dev/mapper/rootfs is active.
  type:    LUKS2
  device:  /dev/loop0p2
  keysize: 512 bits
`
	got := parseCryptsetupStatusDevice(status)
	if got != "/dev/loop0p2" {
		t.Errorf("parseCryptsetupStatusDevice() = %q, want /dev/loop0p2", got)
	}
	if parseCryptsetupStatusDevice("") != "" {
		t.Error("expected empty for empty status output")
	}
}

func TestFdeCrypttabEntry(t *testing.T) {
	t.Parallel()

	got := fdeCrypttabEntry("rootfs", "uuid-1", "/etc/cryptsetup-keys.d/fde.key")
	want := "rootfs UUID=uuid-1 /etc/cryptsetup-keys.d/fde.key luks,discard"
	if got != want {
		t.Errorf("fdeCrypttabEntry() = %q, want %q", got, want)
	}
}

func TestFdeLuksCmdlineParams(t *testing.T) {
	t.Parallel()

	manual := fdeLuksCmdlineParams("uuid-1", "rootfs", "/etc/cryptsetup-keys.d/fde.key", false)
	if len(manual) != 2 {
		t.Fatalf("manual params len = %d, want 2", len(manual))
	}
	if strings.Contains(strings.Join(manual, " "), "rd.luks.key=") {
		t.Error("manual mode should not include rd.luks.key")
	}

	auto := fdeLuksCmdlineParams("uuid-1", "rootfs", "/etc/cryptsetup-keys.d/fde.key", true)
	if len(auto) != 3 {
		t.Fatalf("auto params len = %d, want 3", len(auto))
	}
	if !strings.Contains(auto[2], "rd.luks.key=/etc/cryptsetup-keys.d/fde.key") {
		t.Errorf("auto params missing key path: %v", auto)
	}
}

func TestFdeRewriteCmdlineFields(t *testing.T) {
	t.Parallel()

	fields := []string{
		"root=/dev/mapper/root",
		"console=ttyS0",
	}
	got, err := fdeRewriteCmdlineFields(fields, fdeCmdlineRewriteConfig{
		rootMapper:   "/dev/mapper/rootfs",
		immutability: false,
		luksParams:   []string{"rd.luks.uuid=abc"},
	})
	if err != nil {
		t.Fatalf("fdeRewriteCmdlineFields: %v", err)
	}
	if got[0] != "rd.luks.uuid=abc" {
		t.Errorf("first field = %q, want rd.luks prepended", got[0])
	}
	if got[1] != "root=/dev/mapper/rootfs" {
		t.Errorf("root field = %q", got[1])
	}

	verityFields := []string{
		"systemd.verity_root_data=PARTUUID=old",
		"roothash=/dev/loop0p2-/dev/loop0p4",
	}
	got, err = fdeRewriteCmdlineFields(verityFields, fdeCmdlineRewriteConfig{
		rootMapper:   "/dev/mapper/rootfs",
		hashDev:      "/dev/loop0p4",
		immutability: true,
		luksParams:   []string{"rd.luks.uuid=abc"},
	})
	if err != nil {
		t.Fatalf("fdeRewriteCmdlineFields verity: %v", err)
	}
	if !strings.Contains(strings.Join(got, " "), "systemd.verity_root_data=/dev/mapper/rootfs") {
		t.Errorf("verity data not rewritten: %v", got)
	}
	if !strings.Contains(strings.Join(got, " "), "roothash=/dev/mapper/rootfs-/dev/loop0p4") {
		t.Errorf("roothash not rewritten: %v", got)
	}

	_, err = fdeRewriteCmdlineFields([]string{"roothash=x-y"}, fdeCmdlineRewriteConfig{
		rootMapper:   "/dev/mapper/rootfs",
		immutability: true,
	})
	if err == nil {
		t.Fatal("expected error when verity enabled without hash device")
	}
}

func TestFdeRenderCrypttab(t *testing.T) {
	t.Parallel()

	got := fdeRenderCrypttab([]string{"line1", "line2"})
	if got != "line1\nline2\n" {
		t.Errorf("fdeRenderCrypttab() = %q", got)
	}
}

func TestFdeTargetPartitionIDs(t *testing.T) {
	t.Parallel()

	template := &config.ImageTemplate{
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "boot", MountPoint: "/boot/efi"},
				{ID: "rootfs", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			FDE: config.FDEConfig{Enabled: true, Partitions: []string{"userdata"}},
		},
	}
	if ids := fdeTargetPartitionIDs(template); len(ids) != 1 || ids[0] != "userdata" {
		t.Errorf("explicit partitions = %v, want [userdata]", ids)
	}

	template.SystemConfig.FDE.Partitions = nil
	if ids := fdeTargetPartitionIDs(template); len(ids) != 1 || ids[0] != "rootfs" {
		t.Errorf("default root = %v, want [rootfs]", ids)
	}
}
