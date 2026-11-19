package imageprovision

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// newImageRoot creates a minimal image root with the given files.
func newImageRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func readImageFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}

func assertMode(t *testing.T, root, name string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("stat %s: %v", name, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", name, got, want)
	}
}

const testPasswd = "root:x:0:0:root:/root:/bin/bash\nadmin:x:1001:1002:,,,:/home/admin:/bin/bash\n"

func TestWriteAuthorizedKeys(t *testing.T) {
	root := newImageRoot(t, map[string]string{
		"etc/passwd":                      testPasswd,
		"home/admin/.ssh/authorized_keys": "ssh-ed25519 EXISTING keep\n",
	})
	type chown struct {
		name     string
		uid, gid int
	}
	var chowns []chown
	orig := chownFn
	chownFn = func(_ *os.Root, name string, uid, gid int) error {
		chowns = append(chowns, chown{name, uid, gid})
		return nil
	}
	t.Cleanup(func() { chownFn = orig })

	user := config.UserConfig{Name: "admin", SSHAuthorizedKeys: []string{
		"ssh-ed25519 NEW one", "ssh-ed25519 EXISTING keep", " ssh-ed25519 NEW one ",
	}}
	if err := WriteAuthorizedKeys(root, user); err != nil {
		t.Fatalf("WriteAuthorizedKeys: %v", err)
	}

	got := readImageFile(t, root, "home/admin/.ssh/authorized_keys")
	if want := "ssh-ed25519 EXISTING keep\nssh-ed25519 NEW one\n"; got != want {
		t.Errorf("authorized_keys = %q, want %q", got, want)
	}
	assertMode(t, root, "home/admin/.ssh", 0700)
	assertMode(t, root, "home/admin/.ssh/authorized_keys", 0600)
	if len(chowns) != 2 {
		t.Fatalf("expected 2 chowns, got %+v", chowns)
	}
	for _, c := range chowns {
		if c.uid != 1001 || c.gid != 1002 {
			t.Errorf("chown %s to %d:%d, want 1001:1002", c.name, c.uid, c.gid)
		}
	}
}

func TestWriteAuthorizedKeysErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		passwd string
		user   string
	}{
		{"unknown user", testPasswd, "ghost"},
		{"root home is not usable", "svc:x:5:5::/:/usr/sbin/nologin\n", "svc"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := newImageRoot(t, map[string]string{"etc/passwd": tt.passwd})
			err := WriteAuthorizedKeys(root, config.UserConfig{Name: tt.user, SSHAuthorizedKeys: []string{"k"}})
			if err == nil {
				t.Error("expected an error")
			}
		})
	}
	// No keys is a no-op even without an image root.
	if err := WriteAuthorizedKeys("/nonexistent", config.UserConfig{Name: "a"}); err != nil {
		t.Errorf("no keys must be a no-op, got %v", err)
	}
}

func TestWriteAuthorizedKeysRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	root := newImageRoot(t, map[string]string{"etc/passwd": testPasswd})
	if err := os.MkdirAll(filepath.Join(root, "home"), 0755); err != nil {
		t.Fatal(err)
	}
	// A home directory symlinked to an absolute host path must not be followed.
	if err := os.Symlink(outside, filepath.Join(root, "home", "admin")); err != nil {
		t.Fatal(err)
	}
	err := WriteAuthorizedKeys(root, config.UserConfig{Name: "admin", SSHAuthorizedKeys: []string{"k"}})
	if err == nil {
		t.Fatal("expected the escaping symlink to be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(outside, ".ssh")); statErr == nil {
		t.Error("keys were written outside the image root")
	}
}

func TestWriteAuthorizedKeysRejectsInImageSymlink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{"home links to another account", func(t *testing.T, root string) {
			mustSymlink(t, "../root", filepath.Join(root, "home", "admin"))
		}},
		{"home parent links elsewhere", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "home")); err != nil {
				t.Fatal(err)
			}
			mustSymlink(t, "root", filepath.Join(root, "home"))
			mustMkdir(t, filepath.Join(root, "root", "admin"))
		}},
		{".ssh links elsewhere", func(t *testing.T, root string) {
			mustMkdir(t, filepath.Join(root, "home", "admin"))
			mustSymlink(t, "../../root/.ssh", filepath.Join(root, "home", "admin", ".ssh"))
		}},
		{"authorized_keys links elsewhere", func(t *testing.T, root string) {
			mustMkdir(t, filepath.Join(root, "home", "admin", ".ssh"))
			mustSymlink(t, "../../../root/keys", filepath.Join(root, "home", "admin", ".ssh", "authorized_keys"))
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := newImageRoot(t, map[string]string{"etc/passwd": testPasswd})
			mustMkdir(t, filepath.Join(root, "home"))
			mustMkdir(t, filepath.Join(root, "root", ".ssh"))
			tt.setup(t, root)
			err := WriteAuthorizedKeys(root, config.UserConfig{Name: "admin", SSHAuthorizedKeys: []string{"k"}})
			if err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("err = %v, want symbolic link rejection", err)
			}
			if _, statErr := os.Stat(filepath.Join(root, "root", ".ssh", "authorized_keys")); statErr == nil {
				t.Error("key was installed into root's authorized_keys")
			}
		})
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestWriteProxyConfig(t *testing.T) {
	t.Parallel()
	root := newImageRoot(t, map[string]string{
		"etc/environment": "PATH=\"/usr/bin:/bin\"\nhttp_proxy=\"http://old:1\"\nexport HTTPS_PROXY=old\n",
		aptProxyFile:      `Acquire::http::Proxy "http://old:3128";`,
	})
	p := config.ProxyConfig{
		HTTPProxy:  "http://proxy.example.com:3128",
		HTTPSProxy: "http://proxy.example.com:3129",
		NoProxy:    "localhost,mirror.local,.example.com,10.0.0.0/8",
	}
	if err := WriteProxyConfig(root, p); err != nil {
		t.Fatalf("WriteProxyConfig: %v", err)
	}

	env := readImageFile(t, root, "etc/environment")
	wantEnv := "PATH=\"/usr/bin:/bin\"\n" +
		"http_proxy=\"http://proxy.example.com:3128\"\nHTTP_PROXY=\"http://proxy.example.com:3128\"\n" +
		"https_proxy=\"http://proxy.example.com:3129\"\nHTTPS_PROXY=\"http://proxy.example.com:3129\"\n" +
		"no_proxy=\"localhost,mirror.local,.example.com,10.0.0.0/8\"\n" +
		"NO_PROXY=\"localhost,mirror.local,.example.com,10.0.0.0/8\"\n"
	if env != wantEnv {
		t.Errorf("/etc/environment =\n%s\nwant\n%s", env, wantEnv)
	}

	apt := readImageFile(t, root, aptProxyFile)
	for _, want := range []string{
		`Acquire::http::Proxy "http://proxy.example.com:3128";`,
		`Acquire::https::Proxy "http://proxy.example.com:3129";`,
		`Acquire::http::Proxy::mirror.local "DIRECT";`,
		`Acquire::https::Proxy::localhost "DIRECT";`,
	} {
		if !strings.Contains(apt, want) {
			t.Errorf("apt proxy config missing %q:\n%s", want, apt)
		}
	}
	for _, unwanted := range []string{"http://old:3128", "ftp::Proxy", "Proxy::.example.com", "10.0.0.0"} {
		if strings.Contains(apt, unwanted) {
			t.Errorf("apt proxy config must not contain %q:\n%s", unwanted, apt)
		}
	}

	systemd := readImageFile(t, root, systemdProxyFile)
	if !strings.HasPrefix(systemd, "[Manager]\nDefaultEnvironment=") ||
		!strings.Contains(systemd, `"HTTPS_PROXY=http://proxy.example.com:3129"`) ||
		!strings.Contains(systemd, `"no_proxy=localhost,mirror.local,.example.com,10.0.0.0/8"`) {
		t.Errorf("systemd proxy config = %q", systemd)
	}
}

func TestWriteProxyConfigNoProxyOnly(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		t.Run(strconv.FormatBool(existing), func(t *testing.T) {
			t.Parallel()
			otherAptFile := "etc/apt/apt.conf.d/90other-proxy"
			oldProxy := `Acquire::http::Proxy "http://old:3128";`
			files := map[string]string{}
			if existing {
				files[aptProxyFile] = oldProxy
				files[otherAptFile] = oldProxy
			}
			root := newImageRoot(t, files)

			if err := WriteProxyConfig(root, config.ProxyConfig{NoProxy: "localhost"}); err != nil {
				t.Fatalf("WriteProxyConfig: %v", err)
			}

			if _, err := os.Stat(filepath.Join(root, aptProxyFile)); !os.IsNotExist(err) {
				t.Errorf("apt proxy config must be absent without a proxy URL, got %v", err)
			}
			if existing {
				if got := readImageFile(t, root, otherAptFile); got != oldProxy {
					t.Errorf("unrelated apt proxy config = %q, want %q", got, oldProxy)
				}
			}
		})
	}
	if err := WriteProxyConfig("/nonexistent", config.ProxyConfig{}); err != nil {
		t.Errorf("empty proxy must be a no-op, got %v", err)
	}
}

func TestWriteProxyConfigAptRemoveFailure(t *testing.T) {
	t.Parallel()
	root := newImageRoot(t, map[string]string{aptProxyFile + "/child": "content"})

	err := WriteProxyConfig(root, config.ProxyConfig{NoProxy: "localhost"})

	if err == nil || !strings.Contains(err.Error(), "remove apt proxy config") {
		t.Fatalf("WriteProxyConfig error = %v, want apt proxy removal error", err)
	}
}

func TestConfigureCloudInit(t *testing.T) {
	t.Parallel()
	t.Run("generates meta-data and user-data", func(t *testing.T) {
		t.Parallel()
		root := newImageRoot(t, nil)
		if err := ConfigureCloudInit(root, config.CloudInitConfig{Enabled: enabled()}, "edge-01"); err != nil {
			t.Fatalf("ConfigureCloudInit: %v", err)
		}
		ds := readImageFile(t, root, "etc/cloud/cloud.cfg.d/"+config.CloudInitDatasourceFile)
		if !strings.Contains(ds, "datasource_list: [ NoCloud, None ]") {
			t.Errorf("datasource config = %q", ds)
		}
		// meta-data is generated per deployment at first boot, never baked in.
		if _, err := os.Stat(filepath.Join(root, "var/lib/cloud/seed/nocloud/meta-data")); err == nil {
			t.Errorf("meta-data must not be baked into the image: the instance-id would be shared by every clone")
		}
		script := readImageFile(t, root, "usr/libexec/ict-cloud-init-meta-data")
		if !strings.Contains(script, "/proc/sys/kernel/random/uuid") || !strings.Contains(script, "local-hostname: edge-01") {
			t.Errorf("meta-data script = %q", script)
		}
		unit := readImageFile(t, root, "etc/systemd/system/ict-cloud-init-meta-data.service")
		if !strings.Contains(unit, "Before=cloud-init-local.service") ||
			!strings.Contains(unit, "ConditionPathExists=!/var/lib/cloud/seed/nocloud/meta-data") {
			t.Errorf("unit = %q", unit)
		}
		if _, err := os.Lstat(filepath.Join(root,
			"etc/systemd/system/multi-user.target.wants/ict-cloud-init-meta-data.service")); err != nil {
			t.Errorf("unit is not enabled: %v", err)
		}
		if ud := readImageFile(t, root, "var/lib/cloud/seed/nocloud/user-data"); !strings.HasPrefix(ud,
			"#cloud-config") {
			t.Errorf("user-data = %q", ud)
		}
	})
	t.Run("supplied meta-data still gets user-data", func(t *testing.T) {
		t.Parallel()
		root := newImageRoot(t, map[string]string{"var/lib/cloud/seed/nocloud/meta-data": "instance-id: custom\n"})
		if err := ConfigureCloudInit(root, config.CloudInitConfig{Enabled: enabled()}, ""); err != nil {
			t.Fatalf("ConfigureCloudInit: %v", err)
		}
		if ud := readImageFile(t, root, "var/lib/cloud/seed/nocloud/user-data"); !strings.HasPrefix(ud, "#cloud-config") {
			t.Errorf("NoCloud needs user-data next to meta-data, got %q", ud)
		}
		if _, err := os.Stat(filepath.Join(root, "usr/libexec/ict-cloud-init-meta-data")); err == nil {
			t.Errorf("no meta-data generator is needed when the template supplies meta-data")
		}
	})
	t.Run("keeps supplied seed", func(t *testing.T) {
		t.Parallel()
		root := newImageRoot(t, map[string]string{
			"var/lib/cloud/seed/nocloud/meta-data": "instance-id: custom\n",
			"var/lib/cloud/seed/nocloud/user-data": "#cloud-config\nruncmd: [true]\n",
		})
		if err := ConfigureCloudInit(root, config.CloudInitConfig{Enabled: enabled()}, ""); err != nil {
			t.Fatalf("ConfigureCloudInit: %v", err)
		}
		if meta := readImageFile(t, root, "var/lib/cloud/seed/nocloud/meta-data"); meta != "instance-id: custom\n" {
			t.Errorf("supplied meta-data was overwritten: %q", meta)
		}
	})
	t.Run("disabled is a no-op", func(t *testing.T) {
		t.Parallel()
		if err := ConfigureCloudInit("/nonexistent", config.CloudInitConfig{}, ""); err != nil {
			t.Errorf("got %v", err)
		}
	})
}

func TestConfigureProvisioningScripts(t *testing.T) {
	t.Parallel()
	root := newImageRoot(t, map[string]string{
		"usr/local/sbin/a.sh": "#!/bin/sh\n",
		"usr/local/sbin/b.sh": "#!/bin/sh\n",
	})
	scripts := []config.ProvisioningScript{
		{Name: "a", Final: "/usr/local/sbin/a.sh"},
		{Name: "b", Final: "/usr/local/sbin/b.sh", Stage: config.ProvisioningStageEveryBoot},
	}
	if err := ConfigureProvisioningScripts(root, scripts, false); err != nil {
		t.Fatalf("ConfigureProvisioningScripts: %v", err)
	}
	assertMode(t, root, "usr/local/sbin/a.sh", 0755)

	unitA := readImageFile(t, root, "etc/systemd/system/ict-provision-000-a.service")
	for _, want := range []string{
		"After=network-online.target cloud-final.service\n",
		"ConditionPathExists=!" + StampDir + "/a\n",
		"ExecStart=/usr/local/sbin/a.sh\n",
		"ExecStartPost=/usr/bin/touch " + StampDir + "/a\n",
	} {
		if !strings.Contains(unitA, want) {
			t.Errorf("first-boot unit missing %q:\n%s", want, unitA)
		}
	}
	unitB := readImageFile(t, root, "etc/systemd/system/ict-provision-001-b.service")
	if !strings.Contains(unitB, "After=network-online.target cloud-final.service ict-provision-000-a.service\n") ||
		!strings.Contains(unitB, "Requires=ict-provision-000-a.service\n") {
		t.Errorf("second unit must be ordered after and require the first:\n%s", unitB)
	}
	if strings.Contains(unitA, "Requires=") {
		t.Errorf("first unit must not require anything:\n%s", unitA)
	}
	if strings.Contains(unitB, "ConditionPathExists") || strings.Contains(unitB, "touch") {
		t.Errorf("every-boot unit must not use a stamp:\n%s", unitB)
	}
	// A script unit that is After=cloud-final.service must not be wanted by
	// multi-user.target: cloud-final.service is After=multi-user.target, so that
	// is an ordering cycle and systemd drops the unit's start job. Only the
	// trigger is enabled, and it starts the scripts in order without blocking.
	for _, unit := range []string{"ict-provision-000-a.service", "ict-provision-001-b.service"} {
		if strings.Contains(readImageFile(t, root, "etc/systemd/system/"+unit), "[Install]") {
			t.Errorf("%s must have no [Install] section", unit)
		}
		if _, err := os.Lstat(filepath.Join(root, wantsDir, unit)); err == nil {
			t.Errorf("%s must not be linked from multi-user.target.wants", unit)
		}
	}
	trigger := readImageFile(t, root, "etc/systemd/system/"+ProvisioningTriggerUnit)
	for _, want := range []string{
		"ExecStart=/usr/bin/systemctl start --no-block ict-provision-000-a.service ict-provision-001-b.service\n",
		"WantedBy=multi-user.target\n",
	} {
		if !strings.Contains(trigger, want) {
			t.Errorf("trigger unit missing %q:\n%s", want, trigger)
		}
	}
	if strings.Contains(trigger, "After=") || strings.Contains(trigger, "cloud-final") {
		t.Errorf("trigger must not be ordered after cloud-final.service:\n%s", trigger)
	}
	target, err := os.Readlink(filepath.Join(root, wantsDir, ProvisioningTriggerUnit))
	if err != nil || target != "/etc/systemd/system/"+ProvisioningTriggerUnit {
		t.Errorf("trigger not enabled: target=%q err=%v", target, err)
	}

	// Re-running replaces the enable symlinks instead of failing.
	if err := ConfigureProvisioningScripts(root, scripts, false); err != nil {
		t.Errorf("second run: %v", err)
	}
}

func TestConfigureProvisioningScriptsMissingScript(t *testing.T) {
	t.Parallel()
	root := newImageRoot(t, nil)
	err := ConfigureProvisioningScripts(root, []config.ProvisioningScript{{Name: "x", Final: "/usr/local/sbin/x.sh"}}, false)
	if err == nil || !strings.Contains(err.Error(), "missing from the image") {
		t.Errorf("err = %v", err)
	}
}

// Every file the writers generate must be off limits to script destinations.
func TestGeneratedFilesAreReservedFromScripts(t *testing.T) {
	t.Parallel()
	unit := ProvisioningUnitName(0, config.ProvisioningScript{Name: "x"})
	for _, generated := range []string{
		"/" + environmentFile, "/" + aptProxyFile, "/" + systemdProxyFile,
		"/" + aptPolicyPrefsFile, "/" + unattendedUpgradesFile,
		"/" + config.CloudInitConfigDir[1:] + "/" + config.CloudInitDatasourceFile,
		"/" + unitDir + "/" + unit, "/" + wantsDir + "/" + unit,
		"/" + unitDir + "/" + ProvisioningTriggerUnit, "/" + wantsDir + "/" + ProvisioningTriggerUnit,
		StampDir + "/x", config.CloudInitSeedDir + "/meta-data",
	} {
		if !config.IsReservedProvisioningPath(generated) {
			t.Errorf("%s is generated by imageprovision but not reserved in config", generated)
		}
	}
}

func TestConfigureProvisioningScriptsRejectsDirectory(t *testing.T) {
	t.Parallel()
	root := newImageRoot(t, nil)
	mustMkdir(t, filepath.Join(root, "usr", "local", "bin"))
	err := ConfigureProvisioningScripts(root, []config.ProvisioningScript{{Name: "x", Final: "/usr/local/bin"}}, false)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("err = %v, want not a regular file", err)
	}
}

func TestWriteAptPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		os        string
		immutable bool
		wantPin   string
	}{
		{name: "mutable image keeps OS upgrades", os: "ubuntu"},
		{name: "immutable ubuntu pins the archive", os: "ubuntu", immutable: true, wantPin: "Pin: release o=Ubuntu"},
		{name: "immutable debian pins the archive", os: "debian", immutable: true, wantPin: "Pin: release o=Debian"},
		{name: "non-apt target is a no-op", os: "azure-linux", immutable: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := newImageRoot(t, nil)
			tmpl := &config.ImageTemplate{
				Target: config.TargetInfo{OS: tt.os},
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{Enabled: tt.immutable},
				},
			}
			if err := WriteAptPolicy(root, tmpl); err != nil {
				t.Fatalf("WriteAptPolicy: %v", err)
			}
			_, statErr := os.Stat(filepath.Join(root, aptPolicyPrefsFile))
			if tt.wantPin == "" {
				if statErr == nil {
					t.Error("no preferences file expected")
				}
				return
			}
			prefs := readImageFile(t, root, aptPolicyPrefsFile)
			if !strings.Contains(prefs, tt.wantPin+"\nPin-Priority: 50") {
				t.Errorf("preferences = %q, want %q at priority 50", prefs, tt.wantPin)
			}
			if uu := readImageFile(t, root, unattendedUpgradesFile); !strings.Contains(uu,
				"#clear Unattended-Upgrade::Allowed-Origins;") {
				t.Errorf("unattended-upgrades config = %q", uu)
			}
		})
	}
}

func enabled() *bool {
	v := true
	return &v
}

func TestRenderProvisioningUnitEscapesPercent(t *testing.T) {
	t.Parallel()
	unit := renderProvisioningUnit(config.ProvisioningScript{Name: "s", Final: "/opt/setup%20script"}, "", false)
	if !strings.Contains(unit, "ExecStart=/opt/setup%%20script\n") {
		t.Errorf("literal %% must be escaped for systemd:\n%s", unit)
	}
}

func TestConfigureProvisioningScriptsBeforeLogin(t *testing.T) {
	t.Parallel()
	root := newImageRoot(t, map[string]string{
		"usr/local/sbin/a.sh": "#!/bin/sh\n",
		"usr/local/sbin/b.sh": "#!/bin/sh\n",
	})
	scripts := []config.ProvisioningScript{
		{Name: "a", Final: "/usr/local/sbin/a.sh"},
		{Name: "b", Final: "/usr/local/sbin/b.sh", TimeoutSec: 120, RebootAfter: true},
	}
	if err := ConfigureProvisioningScripts(root, scripts, true); err != nil {
		t.Fatalf("ConfigureProvisioningScripts: %v", err)
	}
	unitA := readImageFile(t, root, "etc/systemd/system/ict-provision-000-a.service")
	for _, want := range []string{
		"After=network-online.target cloud-config.service\n",
		"Before=systemd-user-sessions.service apt-daily.service apt-daily-upgrade.service\n",
		"ConditionPathExists=!" + StampDir + "/a\n",
		"TimeoutStartSec=" + strconv.Itoa(config.DefaultBeforeLoginTimeoutSec) + "\n",
		"[Install]\nWantedBy=multi-user.target\n",
	} {
		if !strings.Contains(unitA, want) {
			t.Errorf("before-login unit missing %q:\n%s", want, unitA)
		}
	}
	// cloud-final.service is After=multi-user.target; ordering a unit that is
	// before systemd-user-sessions.service after it would be a cycle.
	if strings.Contains(unitA, "cloud-final") {
		t.Errorf("before-login unit must not wait for cloud-final.service:\n%s", unitA)
	}
	unitB := readImageFile(t, root, "etc/systemd/system/ict-provision-001-b.service")
	for _, want := range []string{
		"After=network-online.target cloud-config.service ict-provision-000-a.service\n",
		"Requires=ict-provision-000-a.service\n",
		"TimeoutStartSec=120\n",
		"ExecStartPost=/usr/bin/systemctl reboot --no-block\n",
	} {
		if !strings.Contains(unitB, want) {
			t.Errorf("second before-login unit missing %q:\n%s", want, unitB)
		}
	}
	for _, unit := range []string{"ict-provision-000-a.service", "ict-provision-001-b.service"} {
		target, err := os.Readlink(filepath.Join(root, wantsDir, unit))
		if err != nil || target != "/etc/systemd/system/"+unit {
			t.Errorf("%s not enabled: target=%q err=%v", unit, target, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "etc/systemd/system", ProvisioningTriggerUnit)); err == nil {
		t.Error("before-login mode must not install the trigger unit")
	}
	if _, err := os.Lstat(filepath.Join(root, wantsDir, ProvisioningTriggerUnit)); err == nil {
		t.Error("before-login mode must not enable the trigger unit")
	}
}

func TestRenderProvisioningUnitDefaultModeUnchanged(t *testing.T) {
	t.Parallel()
	got := renderProvisioningUnit(config.ProvisioningScript{Name: "s", Final: "/usr/local/sbin/s.sh"}, "", false)
	want := "# Generated by image-composer-tool\n[Unit]\n" +
		"Description=ICT provisioning script s\n" +
		"Wants=network-online.target\nAfter=network-online.target cloud-final.service\n" +
		"After=setup-overlay.service\n" +
		"ConditionPathExists=!" + StampDir + "/s\n" +
		"\n[Service]\nType=oneshot\nRemainAfterExit=yes\n" +
		"ExecStart=/usr/local/sbin/s.sh\n" +
		"ExecStartPost=/usr/bin/mkdir -p " + StampDir + "\n" +
		"ExecStartPost=/usr/bin/touch " + StampDir + "/s\n" +
		"ExecStartPost=-" + config.ProvisioningReportScript + " ok s\n" +
		"ExecStopPost=-" + config.ProvisioningReportScript + " failed s %n retry\n" +
		"StandardOutput=journal+console\nStandardError=journal+console\n"
	if got != want {
		t.Errorf("default-mode unit changed:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderProvisioningUnitArgsAndEnv(t *testing.T) {
	t.Parallel()
	unit := renderProvisioningUnit(config.ProvisioningScript{
		Name:  "s",
		Final: "/usr/local/sbin/s.sh",
		Args:  []string{"--mode", "k3s", "100%"},
		Env:   map[string]string{"B_VAR": "two", "A_VAR": "50%"},
	}, "", false)
	for _, want := range []string{
		"Environment=\"A_VAR=50%%\"\nEnvironment=\"B_VAR=two\"\n", // sorted by name, % escaped
		"ExecStart=/usr/local/sbin/s.sh --mode k3s 100%%\n",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
	plain := renderProvisioningUnit(config.ProvisioningScript{Name: "s", Final: "/usr/local/sbin/s.sh"}, "", false)
	if strings.Contains(plain, "Environment=") || !strings.Contains(plain, "ExecStart=/usr/local/sbin/s.sh\n") {
		t.Errorf("a script without args or env must render as before:\n%s", plain)
	}
}

func TestRenderProvisioningUnitTimeoutAndReboot(t *testing.T) {
	t.Parallel()
	unit := renderProvisioningUnit(config.ProvisioningScript{
		Name: "s", Final: "/usr/local/sbin/s.sh", TimeoutSec: 900, RebootAfter: true,
	}, "", false)
	for _, want := range []string{"TimeoutStartSec=900\n", "ExecStartPost=/usr/bin/systemctl reboot --no-block\n"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
	// The reboot must be queued only after success is recorded in the stamp.
	if strings.Index(unit, "reboot --no-block") < strings.Index(unit, "touch "+StampDir+"/s") {
		t.Errorf("reboot must come after the stamp is written:\n%s", unit)
	}
	plain := renderProvisioningUnit(config.ProvisioningScript{Name: "s", Final: "/usr/local/sbin/s.sh"}, "", false)
	if strings.Contains(plain, "TimeoutStartSec") || strings.Contains(plain, "reboot") {
		t.Errorf("no timeout or reboot unless requested:\n%s", plain)
	}
	// An every-boot script with a timeout has no stamp and still gets the limit.
	every := renderProvisioningUnit(config.ProvisioningScript{
		Name: "e", Final: "/usr/local/sbin/e.sh", Stage: config.ProvisioningStageEveryBoot, TimeoutSec: 30,
	}, "", false)
	if !strings.Contains(every, "TimeoutStartSec=30\n") || strings.Contains(every, "touch") {
		t.Errorf("every-boot unit:\n%s", every)
	}
}

func TestRenderAptProxySkipsUnrenderableNoProxy(t *testing.T) {
	t.Parallel()
	out := renderAptProxy(config.ProxyConfig{
		HTTPProxy: "http://p:1",
		NoProxy:   "mirror.local;,.example.com,10.0.0.0/8,[::1],good.host",
	})
	if strings.Contains(out, "mirror.local") || strings.Contains(out, "[::1]") || strings.Contains(out, "example.com") {
		t.Errorf("only plain hosts may become apt.conf keys:\n%s", out)
	}
	if !strings.Contains(out, `Acquire::http::Proxy::good.host "DIRECT";`) {
		t.Errorf("plain host must still be excluded:\n%s", out)
	}
}

func TestRenderAptProxyFTPExclusions(t *testing.T) {
	t.Parallel()
	withFTP := renderAptProxy(config.ProxyConfig{FTPProxy: "http://p:21", NoProxy: "mirror.local"})
	if !strings.Contains(withFTP, `Acquire::ftp::Proxy::mirror.local "DIRECT";`) {
		t.Errorf("an ftp proxy must honour noProxy:\n%s", withFTP)
	}
	withoutFTP := renderAptProxy(config.ProxyConfig{HTTPProxy: "http://p:1", NoProxy: "mirror.local"})
	if strings.Contains(withoutFTP, "ftp::Proxy") {
		t.Errorf("no ftp entries without an ftp proxy:\n%s", withoutFTP)
	}
}

func TestWriteKeyOnlySudoers(t *testing.T) {
	t.Parallel()
	key := []string{"ssh-ed25519 AAAA"}
	t.Run("grants only key-only sudo users", func(t *testing.T) {
		t.Parallel()
		root := newImageRoot(t, nil)
		users := []config.UserConfig{
			{Name: "admin", Sudo: true, SSHAuthorizedKeys: key},
			{Name: "ops", Sudo: true, Password: "x", SSHAuthorizedKeys: key},
			{Name: "svc", SSHAuthorizedKeys: key},
			{Name: "guest"},
		}
		if err := WriteKeyOnlySudoers(root, users); err != nil {
			t.Fatalf("WriteKeyOnlySudoers: %v", err)
		}
		got := readImageFile(t, root, "etc/sudoers.d/90-ict-key-only-admins")
		if !strings.Contains(got, "admin ALL=(ALL:ALL) NOPASSWD:ALL\n") || strings.Contains(got, "ops") ||
			strings.Contains(got, "svc") || strings.Contains(got, "guest") {
			t.Errorf("sudoers = %q", got)
		}
		info, err := os.Stat(filepath.Join(root, "etc/sudoers.d/90-ict-key-only-admins"))
		if err != nil || info.Mode().Perm() != 0440 {
			t.Errorf("mode = %v (err %v), want 0440", info.Mode().Perm(), err)
		}
	})
	t.Run("nothing to grant writes nothing", func(t *testing.T) {
		t.Parallel()
		root := newImageRoot(t, nil)
		if err := WriteKeyOnlySudoers(root, []config.UserConfig{{Name: "a", Sudo: true, Password: "x"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "etc/sudoers.d")); err == nil {
			t.Errorf("no sudoers drop-in expected")
		}
	})
	t.Run("every name the account contract accepts is written", func(t *testing.T) {
		t.Parallel()
		root := newImageRoot(t, nil)
		users := []config.UserConfig{
			{Name: "Admin", Sudo: true, SSHAuthorizedKeys: key},
			{Name: "ops.admin", Sudo: true, SSHAuthorizedKeys: key},
			{Name: "1admin", Sudo: true, SSHAuthorizedKeys: key},
		}
		if err := WriteKeyOnlySudoers(root, users); err != nil {
			t.Fatalf("WriteKeyOnlySudoers: %v", err)
		}
		got := readImageFile(t, root, "etc/sudoers.d/90-ict-key-only-admins")
		for _, u := range users {
			if !strings.Contains(got, u.Name+" ALL=(ALL:ALL) NOPASSWD:ALL\n") {
				t.Errorf("missing %q in %q", u.Name, got)
			}
		}
	})
	t.Run("admin group membership is sudo access", func(t *testing.T) {
		t.Parallel()
		root := newImageRoot(t, nil)
		users := []config.UserConfig{
			{Name: "viasudo", Groups: []string{"docker", "sudo"}, SSHAuthorizedKeys: key},
			{Name: "viawheel", Groups: []string{"wheel"}, SSHAuthorizedKeys: key},
			{Name: "plain", Groups: []string{"docker"}, SSHAuthorizedKeys: key},
		}
		if err := WriteKeyOnlySudoers(root, users); err != nil {
			t.Fatalf("WriteKeyOnlySudoers: %v", err)
		}
		got := readImageFile(t, root, "etc/sudoers.d/90-ict-key-only-admins")
		if !strings.Contains(got, "viasudo ") || !strings.Contains(got, "viawheel ") || strings.Contains(got, "plain") {
			t.Errorf("sudoers = %q", got)
		}
	})
	t.Run("invalid name is rejected", func(t *testing.T) {
		t.Parallel()
		err := WriteKeyOnlySudoers(newImageRoot(t, nil),
			[]config.UserConfig{{Name: "a b\nroot", Sudo: true, SSHAuthorizedKeys: key}})
		if err == nil {
			t.Error("a name outside the account contract must be rejected")
		}
	})
}

func TestRenderProvisioningUnitReportsOutcome(t *testing.T) {
	t.Parallel()
	// Every-boot scripts report too. The hooks must not change the unit's result ("-"), and the
	// success hook must follow the start commands, so it is cleared before a reboot is queued.
	for _, stage := range []string{"", config.ProvisioningStageEveryBoot} {
		unit := renderProvisioningUnit(config.ProvisioningScript{Name: "s", Final: "/usr/local/sbin/s.sh", Stage: stage}, "", false)
		okAt, failAt := strings.Index(unit, "ExecStartPost=-"), strings.Index(unit, "ExecStopPost=-")
		if !strings.Contains(unit, " ok s\n") || !strings.Contains(unit, " failed s %n retry\n") || okAt < 0 || failAt < okAt {
			t.Errorf("stage %q: hooks missing or misplaced:\n%s", stage, unit)
		}
	}
}

// runReport runs the report script against a temporary root with a stub systemctl.
func runReport(t *testing.T, root, systemState string, env []string, args ...string) error {
	t.Helper()
	return runReportWith(t, root, systemState, true, env, args...)
}

// runReportWith runs the report script. With stubPath its fixed PATH line is
// replaced by the stub directory; without it the script keeps its trusted PATH.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" } // for temp dirs with spaces or quotes

func runReportWith(t *testing.T, root, systemState string, stubPath bool, env []string, args ...string) error {
	t.Helper()
	bin := filepath.Join(root, "bin")
	script := string(reportScript)
	if stubPath {
		const fixed = "PATH=/usr/bin:/bin:/usr/sbin:/sbin\n"
		if !strings.Contains(script, fixed) {
			t.Fatal("the report script must set a fixed, trusted PATH")
		}
		script = strings.Replace(script, fixed, "PATH="+shq(bin+":/usr/bin:/bin")+"\n", 1)
	}
	script = strings.NewReplacer(
		"state=/var/lib/", "state="+shq(root+"/var/lib/"),
		"etc=/etc\n", "etc="+shq(root+"/etc")+"\n",
	).Replace(script)
	path := filepath.Join(root, "report.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\necho \"$@\" >> " + shq(root+"/systemctl.log") + "\n" +
		"[ \"$1\" = is-system-running ] && echo " + systemState + "\n" +
		// show prints the invocation id; start changes it when STUB_START_NEW_ID is
		// set, as a unit that really starts does.
		"if [ \"$1\" = show ]; then\n" +
		"  [ -z \"$STUB_SHOW_FAILS\" ] || exit 1\n" +
		"  cat " + shq(root+"/invocation") + " 2>/dev/null\nfi\n" +
		"if [ \"$1\" = start ]; then\n" +
		"  [ -z \"$STUB_START_NEW_ID\" ] || echo \"id-$$\" > " + shq(root+"/invocation") + "\n" +
		"  exit \"${STUB_START_RC:-0}\"\nfi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", append([]string{path}, args...)...)
	cmd.Env = append([]string{"PATH=" + bin}, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("report %v: %v\n%s", args, err, out)
	}
	return err
}

func mustRunReport(t *testing.T, root, systemState string, env []string, args ...string) {
	t.Helper()
	if err := runReport(t, root, systemState, env, args...); err != nil {
		t.Fatalf("report %v failed", args)
	}
}

func TestReportScript(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	issue := filepath.Join(root, "etc/issue.d/ict-provision-oep.issue")
	motd := filepath.Join(root, "etc/motd.d/ict-provision-oep")
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	read := func(p string) string { return readImageFile(t, "/", strings.TrimPrefix(p, "/")) }
	unit := "ict-provision-002-oep.service"

	for _, name := range []string{"", "../x", ".hidden", "-x", "a/b", "a b"} {
		if err := runReport(t, root, "running", nil, "ok", name); err == nil {
			t.Errorf("name %q must be rejected", name)
		}
	}
	// A termination by the shutdown is not a failure (exit 143, or the TERM signal).
	for _, env := range [][]string{
		{"SERVICE_RESULT=exit-code", "EXIT_STATUS=143"},
		{"SERVICE_RESULT=signal", "EXIT_STATUS=TERM"},
	} {
		mustRunReport(t, root, "stopping", env, "failed", "oep", unit)
		if exists(issue) {
			t.Fatalf("%v: a termination by the shutdown must not record a failure", env)
		}
	}
	// Any other failure while the system is stopping is one (exit 1, or a timeout).
	for _, env := range [][]string{
		{"SERVICE_RESULT=exit-code", "EXIT_STATUS=1"},
		{"SERVICE_RESULT=timeout", "EXIT_STATUS=TERM"},
		{"SERVICE_RESULT=exit-code"},
	} {
		mustRunReport(t, root, "stopping", env, "failed", "oep", unit)
		if !exists(issue) {
			t.Fatalf("%v: a failure that is not the shutdown's termination must be recorded", env)
		}
		mustRunReport(t, root, "running", nil, "ok", "oep")
	}
	mustRunReport(t, root, "running", []string{"SERVICE_RESULT=success"}, "failed", "oep", unit)
	if exists(issue) {
		t.Fatal("a successful result must not record a failure")
	}

	mustRunReport(t, root, "running", []string{"SERVICE_RESULT=exit-code"}, "failed", "oep", unit)
	got := read(issue)
	for _, want := range []string{
		`Provisioning script "oep" failed.`,
		"sudo journalctl -u " + unit,
		"It runs again at the next boot if the scripts before it succeed.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("notice missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\\") {
		t.Errorf("agetty would interpret a backslash in the issue file:\n%s", got)
	}
	if read(motd) != got {
		t.Error("the message of the day must carry the same notice")
	}

	mustRunReport(t, root, "running", nil, "ok", "oep")
	if exists(issue) || exists(motd) {
		t.Fatal("ok must remove the notices")
	}
}

func TestReportScriptIgnoresScriptPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	evil := filepath.Join(root, "evil")
	ran := filepath.Join(root, "evil-ran")
	for _, tool := range []string{"rm", "mkdir", "cp"} {
		put(t, filepath.Join(evil, tool), "#!/bin/sh\ntouch "+shq(ran)+"\n")
		if err := os.Chmod(filepath.Join(evil, tool), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The script keeps its trusted PATH although the environment's puts a template's directory first.
	env := []string{"PATH=" + evil + ":/usr/bin:/bin", "SERVICE_RESULT=exit-code"}
	issue := filepath.Join(root, "etc/issue.d/ict-provision-x.issue")
	for i, args := range [][]string{{"failed", "x", "u.service"}, {"ok", "x"}, {"done", "x"}} {
		if err := runReportWith(t, root, "running", false, env, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if _, err := os.Stat(issue); (err == nil) != (i == 0) {
			t.Fatalf("after %v: notice present = %v, want %v", args, err == nil, i == 0)
		}
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("the helper ran a program from the script's PATH as root")
	}
}

// put writes a file, creating its directories.
func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReportScriptFailureAndStamp(t *testing.T) {
	t.Parallel()
	failed := []string{"SERVICE_RESULT=exit-code"}
	// A unit that fails after its script wrote the stamp loses it, so the script runs again.
	root := t.TempDir()
	stamp := filepath.Join(root, "var/lib/image-composer-tool/provisioned/oep")
	put(t, stamp, "")
	mustRunReport(t, root, "running", failed, "failed", "oep", "u.service")
	if _, err := os.Stat(stamp); err == nil {
		t.Error("a failed unit must not stay marked complete")
	}
	// A stamp that cannot be removed (a non-empty directory) means no retry is promised.
	root = t.TempDir()
	stamp = filepath.Join(root, "var/lib/image-composer-tool/provisioned/oep")
	put(t, filepath.Join(stamp, "x"), "")
	mustRunReport(t, root, "running", failed, "failed", "oep", "u.service")
	got := readImageFile(t, "/", strings.TrimPrefix(filepath.Join(root, "etc/issue.d/ict-provision-oep.issue"), "/"))
	if strings.Contains(got, "runs again at the next boot") || !strings.Contains(got, "removed: "+stamp) {
		t.Errorf("notice for a stamp that cannot be removed:\n%s", got)
	}
}

func TestReportScriptUnwritableNotice(t *testing.T) {
	t.Parallel()
	failed := []string{"SERVICE_RESULT=exit-code"}
	// A file where a notice directory must be makes that notice unwritable.
	blocked := func(dirs ...string) string {
		root := t.TempDir()
		for _, d := range dirs {
			put(t, filepath.Join(root, "etc", d), "")
		}
		return root
	}
	root := blocked("issue.d", "motd.d")
	if err := runReport(t, root, "running", failed, "failed", "oep", "u.service"); err == nil {
		t.Error("a failure notice that cannot be written anywhere must be reported")
	}
	if left, _ := filepath.Glob(filepath.Join(root, "etc", ".ict-provision-*")); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
	// One notice that cannot be written does not remove the other.
	for dir, kept := range map[string]string{
		"motd.d":  "etc/issue.d/ict-provision-oep.issue",
		"issue.d": "etc/motd.d/ict-provision-oep",
	} {
		root := blocked(dir)
		mustRunReport(t, root, "running", failed, "failed", "oep", "u.service")
		if got := readImageFile(t, "/", filepath.Join(root, kept)[1:]); !strings.Contains(got, `"oep" failed`) {
			t.Errorf("with %s unwritable, %s = %q", dir, kept, got)
		}
	}
}

func TestReportScriptTemporaryFileIsNew(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// A template may install a file at any path, including a guessable one.
	guess := filepath.Join(root, "etc/.ict-provision-oep.tmp")
	put(t, guess, "#!/bin/sh\n")
	mustRunReport(t, root, "running", []string{"SERVICE_RESULT=exit-code"}, "failed", "oep", "u.service")
	if got := readImageFile(t, "/", guess[1:]); got != "#!/bin/sh\n" {
		t.Errorf("the template's file was overwritten: %q", got)
	}
	info, err := os.Stat(filepath.Join(root, "etc/issue.d/ict-provision-oep.issue"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("notice mode = %v (%v), want 0644 so login users can read it", info, err)
	}
}

func TestConfigureProvisioningScriptsNoticeDirs(t *testing.T) {
	t.Parallel()
	scripts := []config.ProvisioningScript{{Name: "a", Final: "/usr/local/sbin/a.sh"}}
	root := newImageRoot(t, map[string]string{"usr/local/sbin/a.sh": "#!/bin/sh\n", "etc/motd.d/keep": ""})
	if err := os.Chmod(filepath.Join(root, "etc/motd.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureProvisioningScripts(root, scripts, false); err != nil {
		t.Fatal(err)
	}
	assertMode(t, root, "usr/libexec/ict-provision-report", 0755)
	if got := readImageFile(t, root, "usr/libexec/ict-provision-report"); got != string(reportScript) ||
		!config.IsReservedProvisioningPath(config.ProvisioningReportScript) ||
		!config.IsReservedProvisioningPath(config.ProvisioningFailedDir+"/x") {
		t.Error("the embedded report script must be installed and reserved from script destinations")
	}
	// A new directory is searchable by everyone; an existing one keeps its mode.
	for dir, mode := range map[string]os.FileMode{"etc/issue.d": 0o755, "etc/motd.d": 0o700} {
		if info, err := os.Stat(filepath.Join(root, dir)); err != nil || !info.IsDir() || info.Mode().Perm() != mode {
			t.Errorf("/%s = %v (%v), want a directory with mode %v", dir, info, err, mode)
		}
	}
	// A notice directory that is a regular file fails the build.
	bad := newImageRoot(t, map[string]string{"usr/local/sbin/a.sh": "#!/bin/sh\n", "etc/motd.d": "x"})
	if err := ConfigureProvisioningScripts(bad, scripts, false); err == nil {
		t.Error("a regular file at /etc/motd.d must fail the build")
	}
}

func TestRenderProvisioningUnitStopOnFailure(t *testing.T) {
	t.Parallel()
	retried := renderProvisioningUnit(config.ProvisioningScript{Name: "s", Final: "/usr/local/sbin/s.sh"}, "", false)
	if strings.Contains(retried, "AssertPathExists") || !strings.Contains(retried, " failed s %n retry\n") {
		t.Errorf("a retried script must not be skipped after a failure:\n%s", retried)
	}
	manual := renderProvisioningUnit(config.ProvisioningScript{
		Name: "m", Final: "/usr/local/sbin/m.sh", StopOnFailure: true,
		RecoveryHint: `run "x" --resume 100% $HOME`,
	}, "", false)
	for _, want := range []string{
		// An assertion, not a condition: a failed condition only skips the unit
		// and the units that require it would still run.
		"ConditionPathExists=!" + StampDir + "/m\nAssertPathExists=!" + FailedDir + "/m\n",
		" failed m %n stop \"run \\\"x\\\" --resume 100%% $$HOME\"\n",
	} {
		if !strings.Contains(manual, want) {
			t.Errorf("stop-on-failure unit missing %q:\n%s", want, manual)
		}
	}
}

func TestReportScriptStopMode(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	marker := filepath.Join(root, "var/lib/image-composer-tool/provisioning-failed/oep")
	issue := filepath.Join(root, "etc/issue.d/ict-provision-oep.issue")
	motd := filepath.Join(root, "etc/motd.d/ict-provision-oep")
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	read := func(p string) string { return readImageFile(t, "/", strings.TrimPrefix(p, "/")) }
	failed := []string{"SERVICE_RESULT=exit-code"}
	unit := "ict-provision-000-oep.service"

	// A stop caused by the shutdown records nothing.
	mustRunReport(t, root, "stopping", []string{"SERVICE_RESULT=exit-code", "EXIT_STATUS=143"}, "failed", "oep", unit, "stop")
	if exists(marker) || exists(issue) {
		t.Fatal("a reboot must not record a failure")
	}
	mustRunReport(t, root, "running", failed, "failed", "oep", unit, "stop", "run it by hand")
	for _, p := range []string{marker, issue, motd} {
		if !exists(p) {
			t.Fatalf("%s not written", p)
		}
	}
	if got := read(marker); got != unit+"\n" {
		t.Errorf("marker = %q, want the unit name so retry can start it", got)
	}
	if got := read(issue); !strings.Contains(got, "It does not run again at boot") || !strings.Contains(got, " retry oep\n") ||
		!strings.Contains(got, "Or finish it by hand (run it by hand), then clear this notice: sudo ") ||
		!strings.Contains(got, " done oep\n") {
		t.Errorf("notice:\n%s", got)
	}
	// Without a hint there is no by-hand line.
	mustRunReport(t, root, "running", failed, "failed", "oep", unit, "stop")
	if got := read(issue); strings.Contains(got, "by hand") {
		t.Errorf("notice without a hint:\n%s", got)
	}
	// A retried script's notice says so.
	mustRunReport(t, root, "running", failed, "failed", "oep", unit, "retry")
	if got := read(issue); !strings.Contains(got, "runs again at the next boot") || strings.Contains(got, " retry oep") {
		t.Errorf("notice:\n%s", got)
	}

	// retry removes the failure and starts the recorded unit.
	mustRunReport(t, root, "running", failed, "failed", "oep", unit, "stop")
	if err := os.WriteFile(filepath.Join(root, "systemctl.log"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunReport(t, root, "running", nil, "retry", "oep")
	if exists(marker) {
		t.Fatal("retry must remove the marker so the unit starts")
	}
	if !exists(issue) || !exists(motd) {
		t.Fatal("retry must keep the notices until the script succeeds (its ok hook removes them)")
	}
	if log := read(filepath.Join(root, "systemctl.log")); !strings.HasSuffix(log, "start "+unit+"\n") ||
		!strings.Contains(log, "show -p InvocationID --value "+unit+"\n") {
		t.Errorf("retry ran systemctl %q, want it to read the invocation id and then start %s", log, unit)
	}
	if err := runReport(t, root, "running", nil, "retry", "oep"); err == nil {
		t.Error("retry without a recorded failure must fail")
	}

	mustRunReport(t, root, "running", failed, "failed", "oep", unit, "stop")
	mustRunReport(t, root, "running", nil, "ok", "oep")
	if exists(marker) || exists(issue) || exists(motd) {
		t.Fatal("ok must remove the failure")
	}

	stamp := filepath.Join(root, "var/lib/image-composer-tool/provisioned/oep")
	mustRunReport(t, root, "running", failed, "failed", "oep", unit, "stop")
	mustRunReport(t, root, "running", nil, "done", "oep")
	if exists(marker) || exists(issue) || !exists(stamp) {
		t.Fatal("done must remove the failure and record the script as complete")
	}
}

func TestReportScriptDoneKeepsFailureWhenStampFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	marker := filepath.Join(root, "var/lib/image-composer-tool/provisioning-failed/oep")
	mustRunReport(t, root, "running", []string{"SERVICE_RESULT=exit-code"}, "failed", "oep", "u.service", "stop")
	// A file where the stamp directory must be makes the stamp unwritable.
	if err := os.WriteFile(filepath.Join(root, "var/lib/image-composer-tool/provisioned"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runReport(t, root, "running", nil, "done", "oep"); err == nil {
		t.Fatal("done must fail when the stamp cannot be written")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the failure must stay recorded when the script could not be marked complete")
	}
}

func TestReportScriptRetryKeepsFailureWhenUnitCannotStart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	marker := filepath.Join(root, "var/lib/image-composer-tool/provisioning-failed/oep")
	issue := filepath.Join(root, "etc/issue.d/ict-provision-oep.issue")
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	mustRunReport(t, root, "running", []string{"SERVICE_RESULT=exit-code"}, "failed", "oep", "u.service", "stop")

	// systemd refuses the start (a required unit failed): the unit never runs, so
	// no hook records the failure again. The marker and notice must still be there.
	err := runReport(t, root, "running", []string{"STUB_START_RC=1"}, "retry", "oep")
	if err == nil {
		t.Fatal("retry must report that the unit could not be started")
	}
	if !exists(marker) || !exists(issue) {
		t.Fatal("a retry that could not start the unit must leave the failure recorded")
	}
	if got := readImageFile(t, "/", strings.TrimPrefix(marker, "/")); got != "u.service\n" {
		t.Errorf("restored marker = %q", got)
	}
	// While the unit runs the marker is gone (the unit does not start with it),
	// but the notice stays until the unit's ok hook removes it.
	mustRunReport(t, root, "running", nil, "retry", "oep")
	if exists(marker) || !exists(issue) {
		t.Errorf("after a started retry: marker=%v notice=%v, want the marker gone and the notice kept",
			exists(marker), exists(issue))
	}
}

func TestReportScriptRetryTellsRunFromRejectedStart(t *testing.T) {
	t.Parallel()
	marker := func(root string) string {
		return filepath.Join(root, "var/lib/image-composer-tool/provisioning-failed/oep")
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	tests := []struct {
		name        string
		state       string
		env         []string
		wantMarker  bool
		description string
	}{
		// The retried script asks for a reboot: systemd cancels the start job, so
		// systemctl start fails, but the unit did run and the shutdown hook left the
		// failure unrecorded on purpose. Restoring the marker would block the next boot.
		{"ran and was stopped by the reboot it asked for", "stopping", []string{"STUB_START_RC=1", "STUB_START_NEW_ID=1"}, false, ""},
		// The unit ran and failed: its own hook records it, retry adds nothing.
		{"ran and failed", "running", []string{"STUB_START_RC=1", "STUB_START_NEW_ID=1"}, false, ""},
		// systemd refused the start (a required unit failed): the unit never ran, so
		// no hook fired, and that holds while the system is stopping too.
		{"refused", "running", []string{"STUB_START_RC=1"}, true, ""},
		{"refused while stopping", "stopping", []string{"STUB_START_RC=1"}, true, ""},
		// The invocation id cannot be read: assume a rejection, because a script
		// that stays blocked can be retried again and one that ran unattended cannot.
		{"refused, id unreadable", "running", []string{"STUB_START_RC=1", "STUB_SHOW_FAILS=1"}, true, ""},
		{"ran, id unreadable", "running", []string{"STUB_START_RC=1", "STUB_START_NEW_ID=1", "STUB_SHOW_FAILS=1"}, true, ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			mustRunReport(t, root, "running", []string{"SERVICE_RESULT=exit-code"}, "failed", "oep", "u.service", "stop")
			if err := runReport(t, root, tt.state, tt.env, "retry", "oep"); err == nil {
				t.Fatal("retry must pass on the systemctl failure")
			}
			if got := exists(marker(root)); got != tt.wantMarker {
				t.Errorf("marker restored = %v, want %v", got, tt.wantMarker)
			}
		})
	}
}

func TestReportScriptNeverBlocksSilently(t *testing.T) {
	t.Parallel()
	failed := []string{"SERVICE_RESULT=exit-code"}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	read := func(p string) string { return readImageFile(t, "/", strings.TrimPrefix(p, "/")) }

	// The marker cannot be written: a script that must not be retried is not
	// blocked, and the notice says it runs again.
	root := t.TempDir()
	state := filepath.Join(root, "var/lib/image-composer-tool")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "provisioning-failed"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunReport(t, root, "running", failed, "failed", "oep", "u.service", "stop")
	notice := read(filepath.Join(root, "etc/issue.d/ict-provision-oep.issue"))
	if !strings.Contains(notice, "runs again at the next boot") || strings.Contains(notice, "does not run again") {
		t.Errorf("without a marker the notice must not claim the script is blocked:\n%s", notice)
	}

	// No notice can be written: remove the marker so the script is retried
	// instead of blocked without anyone being told.
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"issue.d", "motd.d"} {
		if err := os.WriteFile(filepath.Join(root, "etc", d), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := runReport(t, root, "running", failed, "failed", "oep", "u.service", "stop"); err == nil {
		t.Error("a failure notice that cannot be written must be reported")
	}
	if exists(filepath.Join(root, "var/lib/image-composer-tool/provisioning-failed/oep")) {
		t.Error("a blocking marker must not stay when no notice could be shown")
	}
}

func TestReportScriptMarkerNamesDoNotCollide(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "var/lib/image-composer-tool/provisioning-failed")
	failed := []string{"SERVICE_RESULT=exit-code"}
	// "alpha.tmp" is a valid script name: recording the failure of "alpha" must
	// not touch its marker.
	mustRunReport(t, root, "running", failed, "failed", "alpha.tmp", "a.service", "stop")
	mustRunReport(t, root, "running", failed, "failed", "alpha", "b.service", "retry")
	if got := readImageFile(t, "/", strings.TrimPrefix(filepath.Join(dir, "alpha.tmp"), "/")); got != "a.service\n" {
		t.Errorf("marker of alpha.tmp = %q, want it kept", got)
	}
	// The same for retry's saved copy: "alpha.retry" keeps its marker.
	mustRunReport(t, root, "running", failed, "failed", "alpha.retry", "c.service", "stop")
	mustRunReport(t, root, "running", nil, "retry", "alpha")
	if _, err := os.Stat(filepath.Join(dir, "alpha.retry")); err != nil {
		t.Error("retrying alpha removed the marker of alpha.retry")
	}
}
