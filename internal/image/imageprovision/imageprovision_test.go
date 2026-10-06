package imageprovision

import (
	"os"
	"path/filepath"
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
	for _, unwanted := range []string{"ftp::Proxy", "Proxy::.example.com", "10.0.0.0"} {
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
	root := newImageRoot(t, nil)
	if err := WriteProxyConfig(root, config.ProxyConfig{NoProxy: "localhost"}); err != nil {
		t.Fatalf("WriteProxyConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, aptProxyFile)); err == nil {
		t.Error("apt proxy config must not be written without a proxy URL")
	}
	if err := WriteProxyConfig("/nonexistent", config.ProxyConfig{}); err != nil {
		t.Errorf("empty proxy must be a no-op, got %v", err)
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
	if err := ConfigureProvisioningScripts(root, scripts); err != nil {
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
	if err := ConfigureProvisioningScripts(root, scripts); err != nil {
		t.Errorf("second run: %v", err)
	}
}

func TestConfigureProvisioningScriptsMissingScript(t *testing.T) {
	t.Parallel()
	root := newImageRoot(t, nil)
	err := ConfigureProvisioningScripts(root, []config.ProvisioningScript{{Name: "x", Final: "/usr/local/sbin/x.sh"}})
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
	err := ConfigureProvisioningScripts(root, []config.ProvisioningScript{{Name: "x", Final: "/usr/local/bin"}})
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
	unit := renderProvisioningUnit(config.ProvisioningScript{Name: "s", Final: "/opt/setup%20script"}, "")
	if !strings.Contains(unit, "ExecStart=/opt/setup%%20script\n") {
		t.Errorf("literal %% must be escaped for systemd:\n%s", unit)
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
