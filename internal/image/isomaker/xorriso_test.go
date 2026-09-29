package isomaker

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestBuildXorrisoCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        xorrisoArgs
		mustContain []string
		mustNot     []string
	}{
		{
			name: "hybrid_bios_and_uefi",
			args: xorrisoArgs{
				installRoot:      "/work/root",
				isoFilePath:      "/out/image.iso",
				efiFatImgPath:    "/work/root/boot/efiboot.img",
				efiFatImgRelPath: "/boot/efiboot.img",
				biosImgRelPath:   "boot/grub/bios.img",
			},
			mustContain: []string{
				"xorriso -as mkisofs -iso-level 3 ",
				"-b 'boot/grub/bios.img'",
				"--grub2-mbr '/work/root/boot/grub/boot_hybrid.img'",
				"-eltorito-alt-boot -e '/boot/efiboot.img'",
				"-append_partition 2 0xef '/work/root/boot/efiboot.img'",
				`-volid '` + IsoLabel + `'`,
				`-o '/out/image.iso' '/work/root'`,
			},
			mustNot: []string{"--efi-boot ", "-allow-limited-size"},
		},
		{
			name: "uefi_only",
			args: xorrisoArgs{
				installRoot:      "/work/root",
				isoFilePath:      "/out/image.iso",
				efiFatImgPath:    "/work/root/boot/efiboot.img",
				efiFatImgRelPath: "/boot/efiboot.img",
			},
			mustContain: []string{
				"xorriso -as mkisofs -iso-level 3 ",
				"--efi-boot '/work/root/boot/efiboot.img'",
				"-efi-boot-part --efi-boot-image --protective-msdos-label",
				`-o '/out/image.iso' '/work/root'`,
			},
			mustNot: []string{"--grub2-mbr", "-eltorito-alt-boot", "-allow-limited-size"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := buildXorrisoCommand(tt.args)
			for _, want := range tt.mustContain {
				if !strings.Contains(cmd, want) {
					t.Errorf("command missing %q:\n%s", want, cmd)
				}
			}
			for _, bad := range tt.mustNot {
				if strings.Contains(cmd, bad) {
					t.Errorf("command must not contain %q:\n%s", bad, cmd)
				}
			}
		})
	}
}

func TestBuildXorrisoCommandQuotesHostilePaths(t *testing.T) {
	t.Parallel()
	hostile := "/work dir/$(touch /pwned)/it's"
	for _, bios := range []string{"", "boot/grub/bios.img"} {
		cmd := buildXorrisoCommand(xorrisoArgs{
			installRoot:      hostile + "/root",
			isoFilePath:      hostile + "/image.iso",
			efiFatImgPath:    hostile + "/root/boot/efiboot.img",
			efiFatImgRelPath: "/boot/efiboot.img",
			biosImgRelPath:   bios,
		})
		quoted := shell.QuoteArg(hostile + "/image.iso")
		if !strings.Contains(cmd, "-o "+quoted) {
			t.Errorf("bios=%q: ISO path not shell-quoted:\n%s", bios, cmd)
		}
		if strings.Contains(cmd, `"`) {
			t.Errorf("bios=%q: command must not double-quote arguments:\n%s", bios, cmd)
		}
	}
}

func TestIsoAdditionalFileName(t *testing.T) {
	t.Parallel()
	// The staged basename must equal the source basename: the installer's cp
	// then puts a file destined for an existing directory ("/etc") at
	// /etc/<basename>, as a raw build does.
	tests := []struct{ local, final, base string }{
		{"/src/user-data", "/var/lib/cloud/seed/nocloud/user-data", "user-data"},
		{"/src/motd", "/etc", "motd"},
		{"/src/motd", "/etc/", "motd"},
		{"/src/motd", "/", "motd"},
	}
	for _, tt := range tests {
		got := isoAdditionalFileName(tt.local, tt.final)
		if filepath.Base(got) != tt.base || strings.Count(got, "/") != 1 {
			t.Errorf("isoAdditionalFileName(%q, %q) = %q, want <dir>/%s", tt.local, tt.final, got, tt.base)
		}
	}
	// The same destination (with or without the trailing slash or dot
	// segments) shares a directory; different destinations do not, even when
	// one is the parent of the other.
	same := isoAdditionalFileName("/src/a", "/etc")
	for _, final := range []string{"/etc/", "/usr/../etc"} {
		if got := isoAdditionalFileName("/src/a", final); got != same {
			t.Errorf("final %q staged at %q, want %q", final, got, same)
		}
	}
	if isoAdditionalFileName("/src/motd", "/etc") == isoAdditionalFileName("/src/motd", "/etc/motd") {
		t.Errorf("a destination and its child must not share a staging path")
	}
	if isoAdditionalFileName("/a/cfg", "/etc/a.cfg") == isoAdditionalFileName("/b/cfg", "/etc/b.cfg") {
		t.Errorf("files sharing a basename must not share a staging path")
	}
}

func TestRewriteProvisioningSourcesForISO(t *testing.T) {
	t.Parallel()
	enabled := true
	tmpl := &config.ImageTemplate{SystemConfig: config.SystemConfig{
		Provisioning: config.ProvisioningConfig{Scripts: []config.ProvisioningScript{
			{Name: "s", Local: "/home/builder/src/setup.sh", Final: "/usr/local/sbin/setup.sh"},
		}},
		CloudInit: config.CloudInitConfig{Enabled: &enabled, UserDataFile: "/home/builder/user-data",
			ConfigFiles: []string{"/home/builder/99.cfg"}},
		Users: []config.UserConfig{{Name: "admin", SSHAuthorizedKeysFiles: []string{"/home/builder/admin.pub"}}},
	}}
	rewriteProvisioningSourcesForISO(tmpl)
	sc := tmpl.SystemConfig
	if got := sc.Provisioning.Scripts[0].Local; got != "../additionalfiles/"+isoAdditionalFileName("/home/builder/src/setup.sh", "/usr/local/sbin/setup.sh") {
		t.Errorf("script source = %q, want the on-ISO copy", got)
	}
	if sc.CloudInit.UserDataFile != "" || sc.CloudInit.ConfigFiles != nil || !sc.CloudInit.IsEnabled() {
		t.Errorf("cloud-init host paths must be dropped and enabled kept: %+v", sc.CloudInit)
	}
	if sc.Users[0].SSHAuthorizedKeysFiles != nil {
		t.Errorf("SSH key file paths must be dropped: %+v", sc.Users[0])
	}
}

func TestBuildIsoImageStopsBeforeBuildWithoutSudoCredential(t *testing.T) {
	t.Parallel()
	// A nil InitrdMaker proves the check runs before any build step.
	isoMaker := &IsoMaker{template: &config.ImageTemplate{SystemConfig: config.SystemConfig{
		Users: []config.UserConfig{{Name: "admin", Sudo: true}},
	}}}
	err := isoMaker.BuildIsoImage()
	if err == nil || !strings.Contains(err.Error(), "no password or SSH authorized key") {
		t.Fatalf("err = %v, want a missing-credential error", err)
	}
}
