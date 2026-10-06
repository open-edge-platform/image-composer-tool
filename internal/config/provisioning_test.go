package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Public keys (no private halves exist) used as valid authorized_keys lines.
const (
	testKey1 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGgL6aAgYAGnMFok5OwHz7Q3u1QmAEkYOi+X1am2kd8g"
	testKey2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINt0WME+wQDY/FxCWDXoaSmKjhhvLHIZ5O7y6xA5C5xK"
	testKey3 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIH3uagmfEvhAXHpsBjLa0viK7Ts0/PvY/nAvDwaSOFUc"
	testKey4 = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBJtVSdva" +
		"cJfZbBmYGC7M0L9QPNIjSpQBYulv519uY2uaEbdSfCl+k/U1oR6vsjLW96jZobjwwbyby22/WYjRIUs="
)

const provisioningTemplateYAML = `image:
  name: platform
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: iso
systemConfig:
  name: platform
  hostname: platform
  bootloader:
    bootEntryPolicy: exclusive
  users:
    - name: admin
      shell: /bin/zsh
      home: /srv/admin
      sshAuthorizedKeys:
        - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGgL6aAgYAGnMFok5OwHz7Q3u1QmAEkYOi+X1am2kd8g admin@example
  proxy:
    httpProxy: http://proxy.example.com:3128
    noProxy: localhost,.example.com
  cloudInit:
    enabled: true
  provisioning:
    scripts:
      - name: second
        local: second.sh
        final: /usr/local/sbin/second.sh
        order: 20
      - name: first
        local: first.sh
        final: /usr/local/sbin/first.sh
        stage: every-boot
        order: 10
  aptPolicy:
    upgradeAllowedRepos:
      - platform
`

func TestParseProvisioningSections(t *testing.T) {
	t.Parallel()
	tmpl, err := parseYAMLTemplate([]byte(provisioningTemplateYAML), false)
	if err != nil {
		t.Fatalf("parseYAMLTemplate: %v", err)
	}
	sc := tmpl.SystemConfig
	if sc.Proxy.HTTPProxy != "http://proxy.example.com:3128" || sc.Proxy.NoProxy != "localhost,.example.com" {
		t.Errorf("proxy not parsed: %+v", sc.Proxy)
	}
	if !sc.CloudInit.IsEnabled() {
		t.Error("cloudInit.enabled not parsed")
	}
	if got := tmpl.GetBootEntryPolicy(); got != BootEntryPolicyExclusive {
		t.Errorf("GetBootEntryPolicy = %q, want exclusive", got)
	}
	if u := sc.Users[0]; u.Shell != "/bin/zsh" || u.Home != "/srv/admin" || len(u.SSHAuthorizedKeys) != 1 {
		t.Errorf("user not parsed: %+v", u)
	}
	scripts := tmpl.SortedProvisioningScripts()
	if len(scripts) != 2 || scripts[0].Name != "first" || scripts[1].Name != "second" {
		t.Errorf("SortedProvisioningScripts order = %+v", scripts)
	}
	if got := sc.AptPolicy.UpgradeAllowedRepos; len(got) != 1 || got[0] != "platform" {
		t.Errorf("aptPolicy not parsed: %+v", sc.AptPolicy)
	}
}

func TestProvisioningSchemaRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, find, replace string }{
		{"unknown proxy field", "noProxy:", "socksProxy:"},
		{"invalid stage", "stage: every-boot", "stage: sometimes"},
		{"invalid boot entry policy", "bootEntryPolicy: exclusive", "bootEntryPolicy: wipe"},
		{"multi-line ssh key", "- " + testKey1 + " admin@example",
			"- \"" + testKey1 + " admin@example\\n" + testKey2 + "\""},
		{"private key", "- " + testKey1 + " admin@example", "- \"-----BEGIN OPENSSH PRIVATE KEY-----\""},
		{"invalid key blob", "- " + testKey1 + " admin@example", "- ssh-ed25519 not-a-key admin@example"},
		{"invalid hostname", "hostname: platform", "hostname: \"edge: 01\""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yaml := strings.Replace(provisioningTemplateYAML, tt.find, tt.replace, 1)
			if yaml == provisioningTemplateYAML {
				t.Fatalf("test input %q not found in the template", tt.find)
			}
			if _, err := parseYAMLTemplate([]byte(yaml), false); err == nil {
				t.Fatal("expected template to be rejected")
			}
		})
	}
}

func TestValidateSSHKeyLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line    string
		wantErr bool
	}{
		{testKey1 + " user@host", false},
		{testKey4, false},
		{`from="10.0.0.0/8",no-pty ` + testKey2 + " admin", false},
		{"ssh-ed25519 not-a-key", true},
		{"ssh-rsa " + strings.Fields(testKey1)[1], true}, // blob declares ssh-ed25519
		{"ssh-ed25519 AAAA", true},
		{"not-a-valid-option " + testKey1, true},
		{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5", true}, // algorithm prefix only, truncated key
		{testKey1 + " a " + testKey2, false},       // the comment may contain anything
		{"restrict,command=\"/bin/true\" " + testKey1, false},
		{"-----BEGIN OPENSSH PRIVATE KEY-----", true},
		{"b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ", true},
		{"ssh-ed25519", true},
		{testKey1 + "\n" + testKey2, true},
	}
	for _, tt := range tests {
		err := validateSSHKeyLine(tt.line)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateSSHKeyLine(%q) err = %v, wantErr %v", tt.line, err, tt.wantErr)
		}
		if err != nil && strings.Contains(err.Error(), "AAAA") {
			t.Errorf("error must not echo the key: %v", err)
		}
	}
}

func TestValidateHostname(t *testing.T) {
	t.Parallel()
	for host, wantErr := range map[string]bool{
		"": false, "edge-01": false, "node1.example.com": false,
		"-edge": true, "edge_01": true, "edge 01": true, "edge\n01": true, strings.Repeat("a", 64): true,
	} {
		if err := validateHostname(host); (err != nil) != wantErr {
			t.Errorf("validateHostname(%q) err = %v, wantErr %v", host, err, wantErr)
		}
	}
}

func TestValidateProxyConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		proxy   ProxyConfig
		wantErr bool
	}{
		{"empty", ProxyConfig{}, false},
		{"http with port", ProxyConfig{HTTPProxy: "http://proxy:3128"}, false},
		{"credentials rejected", ProxyConfig{HTTPSProxy: "http://user:pw@proxy:8080"}, true},
		{"query string rejected", ProxyConfig{HTTPProxy: "http://proxy/?token=secret"}, true},
		{"fragment rejected", ProxyConfig{HTTPProxy: "http://proxy:3128/#secret"}, true},
		{"empty query marker rejected", ProxyConfig{HTTPProxy: "http://proxy:3128?"}, true},
		{"socks5", ProxyConfig{FTPProxy: "socks5://proxy:1080"}, false},
		{"no scheme", ProxyConfig{HTTPProxy: "proxy:3128"}, true},
		{"unsupported scheme", ProxyConfig{HTTPProxy: "file:///etc/passwd"}, true},
		{"quote injection", ProxyConfig{HTTPProxy: "http://proxy:3128\";evil"}, true},
		{"newline in noProxy", ProxyConfig{NoProxy: "a\nb"}, true},
		{"space in noProxy", ProxyConfig{NoProxy: "a, b"}, true},
		{"punctuation in noProxy", ProxyConfig{NoProxy: "mirror.local;"}, true},
		{"mixed noProxy entries", ProxyConfig{NoProxy: "localhost,.example.com,10.0.0.0/8,*.lan,"}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validateProxyConfig(tt.proxy); (err != nil) != tt.wantErr {
				t.Errorf("validateProxyConfig(%+v) err = %v, wantErr %v", tt.proxy, err, tt.wantErr)
			}
		})
	}
}

func TestValidateProvisioningScripts(t *testing.T) {
	t.Parallel()
	valid := ProvisioningScript{Name: "setup", Local: "setup.sh", Final: "/usr/local/sbin/setup.sh"}
	tests := []struct {
		name    string
		mutate  func(s *ProvisioningScript)
		dup     bool
		wantErr bool
	}{
		{"valid", func(*ProvisioningScript) {}, false, false},
		{"bad name", func(s *ProvisioningScript) { s.Name = "bad name" }, false, true},
		{"missing local", func(s *ProvisioningScript) { s.Local = "" }, false, true},
		{"relative final", func(s *ProvisioningScript) { s.Final = "setup.sh" }, false, true},
		{"traversal final", func(s *ProvisioningScript) { s.Final = "/usr/../etc/x" }, false, true},
		{"bad stage", func(s *ProvisioningScript) { s.Stage = "later" }, false, true},
		{"negative order", func(s *ProvisioningScript) { s.Order = -1 }, false, true},
		{"duplicate name", func(*ProvisioningScript) {}, true, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := valid
			tt.mutate(&s)
			scripts := []ProvisioningScript{s}
			if tt.dup {
				scripts = append(scripts, s)
			}
			if err := validateProvisioningScripts(scripts); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateProvisioningScriptsDuplicateFinal(t *testing.T) {
	t.Parallel()
	scripts := []ProvisioningScript{
		{Name: "a", Local: "a.sh", Final: "/usr/local/sbin/run.sh"},
		{Name: "b", Local: "b.sh", Final: "/usr/local/sbin/run.sh"},
	}
	err := validateProvisioningScripts(scripts)
	if err == nil || !strings.Contains(err.Error(), "share final path") {
		t.Errorf("err = %v, want shared final path error", err)
	}
}

func TestValidateProvisioningScriptsNestedFinal(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{
		{"/opt/tool", "/opt/tool/run.sh"},
		{"/opt/tool/run.sh", "/opt/tool"},
	} {
		scripts := []ProvisioningScript{
			{Name: "a", Local: "a.sh", Final: pair[0]},
			{Name: "b", Local: "b.sh", Final: pair[1]},
		}
		err := validateProvisioningScripts(scripts)
		if err == nil || !strings.Contains(err.Error(), "inside the other") {
			t.Errorf("finals %v: err = %v, want nested final path error", pair, err)
		}
	}
	// A shared name prefix is not ancestry.
	scripts := []ProvisioningScript{
		{Name: "a", Local: "a.sh", Final: "/opt/tool"},
		{Name: "b", Local: "b.sh", Final: "/opt/tool2/run.sh"},
	}
	if err := validateProvisioningScripts(scripts); err != nil {
		t.Errorf("sibling paths sharing a name prefix: err = %v, want nil", err)
	}
}

func TestValidateProvisioningScriptsReservedFinal(t *testing.T) {
	t.Parallel()
	for _, final := range []string{
		"/etc/environment",
		"/etc/apt/apt.conf.d/95ict-proxy",
		"/etc/systemd/system.conf.d/90-ict-proxy.conf",
		"/etc/systemd/system/ict-provision-000-x.service",
		"/etc/systemd/system/multi-user.target.wants/ict-provision-000-x.service",
		// Descendants and ancestors of generated paths cannot be installed.
		"/etc/environment/setup.sh",
		"/etc/apt/apt.conf.d/95ict-proxy/x.sh",
		"/etc/systemd/system",
		ProvisioningStampDir,
		CloudInitSeedDir,
		ProvisioningStampDir + "/x",
		CloudInitSeedDir + "/user-data",
		CloudInitMetaDataScript,
		"/etc/systemd/system/" + CloudInitMetaDataUnit,
		"/etc/systemd/system/multi-user.target.wants/" + CloudInitMetaDataUnit,
	} {
		err := validateProvisioningScripts([]ProvisioningScript{{Name: "x", Local: "x.sh", Final: final}})
		if err == nil || !strings.Contains(err.Error(), "generated by image-composer-tool") {
			t.Errorf("final %q: err = %v, want reserved path rejection", final, err)
		}
	}
}

func TestValidateUsersShellAndHome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		user    UserConfig
		wantErr bool
	}{
		{"defaults", UserConfig{Name: "a"}, false},
		{"valid shell and home", UserConfig{Name: "a", Shell: "/bin/zsh", Home: "/home/a"}, false},
		{"relative shell", UserConfig{Name: "a", Shell: "zsh"}, true},
		{"home with space", UserConfig{Name: "a", Home: "/home/a b"}, true},
		{"home traversal", UserConfig{Name: "a", Home: "/home/../root"}, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpl := &ImageTemplate{SystemConfig: SystemConfig{Users: []UserConfig{tt.user}}}
			if err := tmpl.validateUsers(); (err != nil) != tt.wantErr {
				t.Errorf("validateUsers err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeProvisioningSections(t *testing.T) {
	t.Parallel()
	enabled := true
	defaults := SystemConfig{
		Proxy:        ProxyConfig{HTTPProxy: "http://default:1"},
		Provisioning: ProvisioningConfig{Scripts: []ProvisioningScript{{Name: "base"}}},
		CloudInit:    CloudInitConfig{Enabled: &enabled},
	}
	user := SystemConfig{
		Proxy: ProxyConfig{HTTPSProxy: "http://user:2"},
		Provisioning: ProvisioningConfig{Scripts: []ProvisioningScript{
			{Name: "extra"}, {Name: "base", Final: "/override"},
		}},
		AptPolicy: AptPolicy{UpgradeAllowedRepos: []string{"r"}},
	}
	merged := mergeSystemConfig(defaults, user)
	if merged.Proxy.HTTPProxy != "" || merged.Proxy.HTTPSProxy != "http://user:2" {
		t.Errorf("proxy should be replaced by the user block, got %+v", merged.Proxy)
	}
	scripts := merged.Provisioning.Scripts
	if len(scripts) != 2 || scripts[0].Name != "base" || scripts[0].Final != "/override" || scripts[1].Name != "extra" {
		t.Errorf("scripts should accumulate, same name overriding in place; got %+v", scripts)
	}
	if !merged.CloudInit.IsEnabled() {
		t.Error("default cloudInit should be kept when the user sets none")
	}
	disabled := false
	child := mergeSystemConfig(defaults, SystemConfig{CloudInit: CloudInitConfig{Enabled: &disabled}})
	if child.CloudInit.IsEnabled() {
		t.Error("an explicit enabled: false must turn off an inherited cloudInit section")
	}
	if len(merged.AptPolicy.UpgradeAllowedRepos) != 1 {
		t.Errorf("aptPolicy not merged: %+v", merged.AptPolicy)
	}
	if isEmptySystemConfig(user) {
		t.Error("a system config with only provisioning sections must not be empty")
	}
}

func TestMergeUserConfigSSHKeys(t *testing.T) {
	t.Parallel()
	merged := mergeUserConfig(
		UserConfig{Name: "a", SSHAuthorizedKeys: []string{"k1"}},
		UserConfig{Name: "a", SSHAuthorizedKeys: []string{"k1", "k2"}, SSHAuthorizedKeysFiles: []string{"/f"}},
	)
	if len(merged.SSHAuthorizedKeys) != 2 || len(merged.SSHAuthorizedKeysFiles) != 1 {
		t.Errorf("merged keys = %+v", merged)
	}
}

func writeProvisioningTestFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLowerProvisioningInputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := writeProvisioningTestFile(t, filepath.Join(dir, "setup.sh"), "#!/bin/sh\n")
	userData := writeProvisioningTestFile(t, filepath.Join(dir, "user-data"), "#cloud-config\n{}\n")
	cfg := writeProvisioningTestFile(t, filepath.Join(dir, "99_custom.cfg"), "a: b\n")
	keys := writeProvisioningTestFile(t, filepath.Join(dir, "admin.pub"),
		"# comment\n"+testKey1+" one\n\n"+testKey2+" two\n")
	enabled := true

	tmpl := &ImageTemplate{SystemConfig: SystemConfig{
		Packages: []string{"openssh-server"},
		Users: []UserConfig{{
			Name: "admin", SSHAuthorizedKeys: []string{testKey1 + " one"}, SSHAuthorizedKeysFiles: []string{keys},
		}},
		Provisioning: ProvisioningConfig{Scripts: []ProvisioningScript{
			{Name: "setup", Local: script, Final: "/usr/local/sbin/setup.sh"},
		}},
		CloudInit: CloudInitConfig{Enabled: &enabled, UserDataFile: userData, ConfigFiles: []string{cfg}},
	}}

	for i := 0; i < 2; i++ { // idempotent
		if err := tmpl.lowerProvisioningInputs(); err != nil {
			t.Fatalf("lowerProvisioningInputs pass %d: %v", i, err)
		}
	}

	finals := map[string]string{}
	for _, f := range tmpl.SystemConfig.AdditionalFiles {
		finals[f.Final] = f.Local
	}
	want := map[string]string{
		"/usr/local/sbin/setup.sh":            script,
		CloudInitSeedDir + "/user-data":       userData,
		CloudInitConfigDir + "/99_custom.cfg": cfg,
	}
	for final, local := range want {
		if finals[final] != local {
			t.Errorf("additionalFiles[%s] = %q, want %q", final, finals[final], local)
		}
	}
	if len(tmpl.SystemConfig.AdditionalFiles) != len(want) {
		t.Errorf("expected %d additional files, got %+v", len(want), tmpl.SystemConfig.AdditionalFiles)
	}
	if n := strings.Count(strings.Join(tmpl.SystemConfig.Packages, " "), cloudInitPackage); n != 1 {
		t.Errorf("cloud-init package added %d times: %v", n, tmpl.SystemConfig.Packages)
	}
	if got := tmpl.SystemConfig.Users[0].SSHAuthorizedKeys; len(got) != 2 || got[1] != testKey2+" two" {
		t.Errorf("SSH keys = %v", got)
	}
}

func TestLowerProvisioningInputsErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	badUserData := writeProvisioningTestFile(t, filepath.Join(dir, "user-data"), "users: []\n")
	privateKey := writeProvisioningTestFile(t, filepath.Join(dir, "id_ed25519"),
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----\n")
	enabled := true
	link := filepath.Join(dir, "link.sh")
	if err := os.Symlink(badUserData, link); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		sc   SystemConfig
		want string
	}{
		{"missing script", SystemConfig{Provisioning: ProvisioningConfig{Scripts: []ProvisioningScript{
			{Name: "s", Local: "missing.sh", Final: "/s"}}}}, "not found"},
		{"symlinked script", SystemConfig{Provisioning: ProvisioningConfig{Scripts: []ProvisioningScript{
			{Name: "s", Local: link, Final: "/s"}}}}, "symlink"},
		{"user-data without header", SystemConfig{
			CloudInit: CloudInitConfig{Enabled: &enabled, UserDataFile: badUserData}}, "#cloud-config"},
		{"missing key file", SystemConfig{
			Users: []UserConfig{{Name: "a", SSHAuthorizedKeysFiles: []string{"/nonexistent/k.pub"}}}}, "sshAuthorizedKeysFiles"},
		{"private key file", SystemConfig{
			Users: []UserConfig{{Name: "a", SSHAuthorizedKeysFiles: []string{privateKey}}}}, "private key"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpl := &ImageTemplate{SystemConfig: tt.sc}
			err := tmpl.lowerProvisioningInputs()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLowerProvisioningInputsRejectsFinalCollision(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := writeProvisioningTestFile(t, filepath.Join(dir, "one", "99_custom.cfg"), "a: b\n")
	second := writeProvisioningTestFile(t, filepath.Join(dir, "two", "99_custom.cfg"), "c: d\n")
	enabled := true
	tmpl := &ImageTemplate{SystemConfig: SystemConfig{
		CloudInit: CloudInitConfig{Enabled: &enabled, ConfigFiles: []string{first, second}},
	}}
	err := tmpl.lowerProvisioningInputs()
	if err == nil || !strings.Contains(err.Error(), "also the destination") {
		t.Errorf("err = %v, want destination collision error", err)
	}
}

func TestAddLoweredFileRejectsOverlappingDestination(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	local := writeProvisioningTestFile(t, filepath.Join(dir, "s.sh"), "#!/bin/sh\n")
	for _, existing := range []string{"/opt/tool", "/opt/tool/run.sh/x"} {
		tmpl := &ImageTemplate{SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{{Local: "/tmp/other", Final: existing}},
		}}
		err := tmpl.addLoweredFile(local, "/opt/tool/run.sh", "owner")
		if err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Errorf("existing %q: err = %v, want overlap error", existing, err)
		}
	}
}

func TestLowerCloudInitRejectsGeneratedDatasourceName(t *testing.T) {
	t.Parallel()
	cfg := writeProvisioningTestFile(t, filepath.Join(t.TempDir(), CloudInitDatasourceFile), "datasource_list: [ Ec2 ]\n")
	enabled := true
	tmpl := &ImageTemplate{SystemConfig: SystemConfig{
		CloudInit: CloudInitConfig{Enabled: &enabled, ConfigFiles: []string{cfg}},
	}}
	err := tmpl.lowerProvisioningInputs()
	if err == nil || !strings.Contains(err.Error(), "would be overwritten") {
		t.Errorf("err = %v, want generated datasource name rejected", err)
	}
}

func TestValidateMergedProvisioning(t *testing.T) {
	t.Parallel()
	repos := []PackageRepository{{ID: "repo1", Codename: "platform"}}
	tests := []struct {
		name    string
		os      string
		allowed []string
		wantErr bool
	}{
		{"by codename", "ubuntu", []string{"platform"}, false},
		{"by id", "ubuntu", []string{"repo1"}, false},
		{"unknown repo", "ubuntu", []string{"other"}, true},
		{"unsupported os", "azure-linux", []string{"platform"}, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpl := &ImageTemplate{
				Target:              TargetInfo{OS: tt.os},
				PackageRepositories: repos,
				SystemConfig:        SystemConfig{AptPolicy: AptPolicy{UpgradeAllowedRepos: tt.allowed}},
			}
			if err := tmpl.validateMergedProvisioning(); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAptPolicyRepoUpgradeAllowed(t *testing.T) {
	t.Parallel()
	repo := PackageRepository{ID: "id1", Codename: "cn1"}
	if !(AptPolicy{}).RepoUpgradeAllowed(repo) {
		t.Error("empty policy must allow every repository")
	}
	if !(AptPolicy{UpgradeAllowedRepos: []string{"cn1"}}).RepoUpgradeAllowed(repo) {
		t.Error("codename match must be allowed")
	}
	if (AptPolicy{UpgradeAllowedRepos: []string{"other"}}).RepoUpgradeAllowed(repo) {
		t.Error("unlisted repository must not be allowed")
	}
}

func TestApplyCompositionOverrides(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyA := writeProvisioningTestFile(t, filepath.Join(dir, "a.pub"), testKey3+" first\n")
	keyB := writeProvisioningTestFile(t, filepath.Join(dir, "b.pub"), testKey4+" second\n")

	tmpl := &ImageTemplate{SystemConfig: SystemConfig{Users: []UserConfig{{Name: "admin"}}}}
	err := tmpl.ApplyCompositionOverrides(CompositionOverrides{
		DiskStrategy: "fastest",
		Hostname:     "cli-host",
		Proxy:        ProxyOverrides{HTTPProxy: strPtr("http://p:1"), NoProxy: strPtr("localhost")},
		SSHKeys:      []SSHKeyOverride{{User: "admin", File: keyA}, {User: "admin", File: keyB}},
	})
	if err != nil {
		t.Fatalf("ApplyCompositionOverrides: %v", err)
	}
	if tmpl.Disk.SelectionPolicy.Strategy != "fastest" ||
		tmpl.SystemConfig.HostName != "cli-host" || tmpl.SystemConfig.Proxy.HTTPProxy != "http://p:1" {
		t.Errorf("overrides not applied: %+v", tmpl)
	}
	keys := tmpl.SystemConfig.Users[0].SSHAuthorizedKeys
	if len(keys) != 2 || keys[0] != testKey3+" first" || keys[1] != testKey4+" second" {
		t.Errorf("CLI SSH keys must be added in flag order: %v", keys)
	}

	errCases := []struct {
		name string
		tmpl *ImageTemplate
		o    CompositionOverrides
	}{
		{"bad strategy", &ImageTemplate{}, CompositionOverrides{DiskStrategy: "random"}},
		{"bad proxy", &ImageTemplate{}, CompositionOverrides{Proxy: ProxyOverrides{HTTPProxy: strPtr("nope")}}},
		{"bad hostname", &ImageTemplate{}, CompositionOverrides{Hostname: "edge: 01"}},
		{"unknown user", &ImageTemplate{}, CompositionOverrides{SSHKeys: []SSHKeyOverride{{User: "ghost", File: keyA}}}},
		{"overlay hostname", &ImageTemplate{Baseline: &Baseline{Mode: BaselineModeOverlay}},
			CompositionOverrides{Hostname: "h"}},
	}
	for _, tt := range errCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.tmpl.ApplyCompositionOverrides(tt.o); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestApplyCompositionOverridesDiskStrategyClearsPath(t *testing.T) {
	t.Parallel()
	tmpl := &ImageTemplate{Disk: DiskConfig{Path: "/dev/sda"}}
	if err := tmpl.ApplyCompositionOverrides(CompositionOverrides{DiskStrategy: "largest"}); err != nil {
		t.Fatalf("ApplyCompositionOverrides: %v", err)
	}
	if tmpl.Disk.Path != "" || tmpl.Disk.SelectionPolicy.Strategy != "largest" {
		t.Errorf("a strategy must clear disk.path so it takes effect: %+v", tmpl.Disk)
	}
}

func TestApplyCompositionOverridesEmptyIsNoOp(t *testing.T) {
	t.Parallel()
	// An invalid value already in the template is not re-validated when no
	// override is given.
	tmpl := &ImageTemplate{SystemConfig: SystemConfig{HostName: "not valid"}}
	if err := tmpl.ApplyCompositionOverrides(CompositionOverrides{}); err != nil {
		t.Errorf("empty overrides must be a no-op, got %v", err)
	}
}

func TestValidateOverlaySystemConfigRejectsProvisioningSections(t *testing.T) {
	t.Parallel()
	tmpl := &ImageTemplate{SystemConfig: SystemConfig{
		Proxy:        ProxyConfig{HTTPProxy: "http://p:1"},
		CloudInit:    CloudInitConfig{Enabled: new(bool)},
		Provisioning: ProvisioningConfig{Scripts: []ProvisioningScript{{Name: "s"}}},
		AptPolicy:    AptPolicy{UpgradeAllowedRepos: []string{"r"}},
	}}
	err := tmpl.validateOverlaySystemConfig()
	if err == nil {
		t.Fatal("expected overlay validation to reject provisioning sections")
	}
	for _, section := range []string{"proxy", "provisioning", "cloudInit", "aptPolicy"} {
		if !strings.Contains(err.Error(), "systemConfig."+section) {
			t.Errorf("error does not name systemConfig.%s: %v", section, err)
		}
	}
}

func TestResolveProvisioningPathsMakesAbsolute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeProvisioningTestFile(t, filepath.Join(dir, "assets", "s.sh"), "#!/bin/sh\n")
	templatePath := filepath.Join(dir, "nested", "t.yml")
	tmpl := &ImageTemplate{SystemConfig: SystemConfig{Provisioning: ProvisioningConfig{Scripts: []ProvisioningScript{
		{Name: "s", Local: "assets/s.sh"}, {Name: "m", Local: "missing.sh"},
	}}}}
	resolveProvisioningPaths(tmpl, templatePath)
	scripts := tmpl.SystemConfig.Provisioning.Scripts
	if want := filepath.Join(dir, "assets", "s.sh"); scripts[0].Local != want {
		t.Errorf("resolved local = %q, want %q (ancestor walk)", scripts[0].Local, want)
	}
	if scripts[1].Local != "missing.sh" {
		t.Errorf("missing file should be left as-is, got %q", scripts[1].Local)
	}
}

func TestValidateDiskPath(t *testing.T) {
	t.Parallel()
	for path, wantErr := range map[string]bool{
		"": false, "/dev/sda": false, "/dev/nvme0n1": false, "/dev/disk/by-id/nvme-Samsung_SSD_990:1": false,
		"sda": true, "/dev/sda; reboot": true, "/dev/$(id)": true, "/dev/../etc/passwd": true, "/dev/sda ": true,
	} {
		if err := validateDiskPath(path); (err != nil) != wantErr {
			t.Errorf("validateDiskPath(%q) err = %v, wantErr %v", path, err, wantErr)
		}
	}
}

func TestProvisioningScriptFinalMustNameFile(t *testing.T) {
	t.Parallel()
	for _, final := range []string{"/", "/usr/local/sbin/"} {
		s := ProvisioningScript{Name: "s", Local: "s.sh", Final: final}
		if err := validateProvisioningScripts([]ProvisioningScript{s}); err == nil {
			t.Errorf("final %q must be rejected", final)
		}
	}
	yaml := strings.Replace(provisioningTemplateYAML, "final: /usr/local/sbin/first.sh", "final: /", 1)
	if _, err := parseYAMLTemplate([]byte(yaml), false); err == nil {
		t.Error("schema must reject final: /")
	}
}

func TestCheckAptPolicyOrigins(t *testing.T) {
	t.Parallel()
	repos := []PackageRepository{
		{ID: "a", URL: "https://ppa.launchpadcontent.net/team/a/ubuntu"},
		{ID: "b", URL: "https://ppa.launchpadcontent.net/team/b/ubuntu"},
		{ID: "c", URL: "https://other.example.com/apt"},
	}
	tests := []struct {
		allowed []string
		wantErr bool
	}{
		{[]string{"a"}, true},
		{[]string{"a", "b"}, false},
		{[]string{"c"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		tmpl := &ImageTemplate{PackageRepositories: repos,
			SystemConfig: SystemConfig{AptPolicy: AptPolicy{UpgradeAllowedRepos: tt.allowed}}}
		if err := tmpl.checkAptPolicyOrigins(); (err != nil) != tt.wantErr {
			t.Errorf("allowed %v: err = %v, wantErr %v", tt.allowed, err, tt.wantErr)
		}
	}
}

func TestAptPolicyExplicitEmptyResetsInherited(t *testing.T) {
	t.Parallel()
	parent := SystemConfig{AptPolicy: AptPolicy{UpgradeAllowedRepos: []string{"a"}}}
	merged := mergeSystemConfig(parent, SystemConfig{AptPolicy: AptPolicy{UpgradeAllowedRepos: []string{}}})
	if merged.AptPolicy.UpgradeAllowedRepos == nil || len(merged.AptPolicy.UpgradeAllowedRepos) != 0 {
		t.Errorf("an explicit empty list must replace the inherited one, got %#v", merged.AptPolicy)
	}
	if !merged.AptPolicy.RepoUpgradeAllowed(PackageRepository{ID: "other"}) {
		t.Error("an explicit empty list must allow every repository")
	}
	omitted := mergeSystemConfig(parent, SystemConfig{})
	if len(omitted.AptPolicy.UpgradeAllowedRepos) != 1 {
		t.Errorf("omitting the section must keep the inherited list, got %#v", omitted.AptPolicy)
	}
}

func TestProvisioningScriptFinalCharacters(t *testing.T) {
	t.Parallel()
	for final, wantErr := range map[string]bool{
		"/usr/local/sbin/setup.sh": false, "/opt/setup%20script": false, "/opt/v1.2/run+once@x": false,
		`/opt/a"b`: true, `/opt/a\b`: true, "/opt/a'b": true, "/opt/a;b": true, "/opt/a$b": true,
	} {
		s := ProvisioningScript{Name: "s", Local: "s.sh", Final: final}
		if err := validateProvisioningScripts([]ProvisioningScript{s}); (err != nil) != wantErr {
			t.Errorf("final %q: err = %v, wantErr %v", final, err, wantErr)
		}
	}
}

func TestValidateUsersPasswordMaxAge(t *testing.T) {
	t.Parallel()
	for age, wantErr := range map[int]bool{0: false, 90: false, -1: true} {
		tmpl := &ImageTemplate{SystemConfig: SystemConfig{Users: []UserConfig{{Name: "a", PasswordMaxAge: intPtr(age)}}}}
		if err := tmpl.validateUsers(); (err != nil) != wantErr {
			t.Errorf("passwordMaxAge %d: err = %v, wantErr %v", age, err, wantErr)
		}
	}
	for age, wantErr := range map[string]bool{"0": false, "30": false, "-1": true} {
		yaml := strings.Replace(provisioningTemplateYAML, "shell: /bin/zsh", "shell: /bin/zsh\n      passwordMaxAge: "+age, 1)
		if _, err := parseYAMLTemplate([]byte(yaml), false); (err != nil) != wantErr {
			t.Errorf("schema passwordMaxAge %s: err = %v, wantErr %v", age, err, wantErr)
		}
	}
}

func TestMergeUserConfigPasswordMaxAgeInheritance(t *testing.T) {
	t.Parallel()
	parent := UserConfig{Name: "a", PasswordMaxAge: intPtr(90)}
	tests := []struct {
		name  string
		child *int
		want  *int
	}{
		{"omitted inherits", nil, intPtr(90)},
		{"explicit zero clears", intPtr(0), intPtr(0)},
		{"explicit value overrides", intPtr(30), intPtr(30)},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mergeUserConfig(parent, UserConfig{Name: "a", PasswordMaxAge: tt.child}).PasswordMaxAge
			if got == nil || *got != *tt.want {
				t.Errorf("PasswordMaxAge = %v, want %d", derefInt(got), *tt.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func TestApplyProxyOverrideExplicitEmptyClears(t *testing.T) {
	t.Parallel()
	tmpl := &ImageTemplate{SystemConfig: SystemConfig{Proxy: ProxyConfig{
		HTTPProxy: "http://p:1", HTTPSProxy: "http://p:2", NoProxy: "localhost",
	}}}
	err := tmpl.ApplyCompositionOverrides(CompositionOverrides{Proxy: ProxyOverrides{
		NoProxy: strPtr(""), HTTPSProxy: strPtr("http://p:3"),
	}})
	if err != nil {
		t.Fatalf("ApplyCompositionOverrides: %v", err)
	}
	if got := tmpl.SystemConfig.Proxy; got.NoProxy != "" || got.HTTPSProxy != "http://p:3" || got.HTTPProxy != "http://p:1" {
		t.Errorf("only set fields may change, an explicit empty clears: %+v", got)
	}
}

func TestValidateUserCredentials(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		user    UserConfig
		wantErr bool
	}{
		{"sudo without credential", UserConfig{Name: "admin", Sudo: true}, true},
		{"sudo with password", UserConfig{Name: "admin", Sudo: true, Password: "x"}, false},
		{"sudo with key", UserConfig{Name: "admin", Sudo: true, SSHAuthorizedKeys: []string{testKey3}}, false},
		{"sudo group without credential", UserConfig{Name: "admin", Groups: []string{"sudo"}}, true},
		{"wheel group without credential", UserConfig{Name: "admin", Groups: []string{"wheel"}}, true},
		{"sudo group with key", UserConfig{Name: "admin", Groups: []string{"sudo"}, SSHAuthorizedKeys: []string{testKey3}}, false},
		{"root without credential", UserConfig{Name: "root"}, true},
		{"root with password", UserConfig{Name: "root", Password: "x"}, false},
		{"root with key", UserConfig{Name: "root", SSHAuthorizedKeys: []string{testKey3}}, false},
		{"root with startup script", UserConfig{Name: "root", StartupScript: "/root/unattendedinstaller"}, false},
		{"other group without credential", UserConfig{Name: "guest", Groups: []string{"docker"}}, false},
		{"no sudo without credential", UserConfig{Name: "guest"}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateUserCredentials([]UserConfig{tt.user}); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeProxyEmptySectionClearsInherited(t *testing.T) {
	t.Parallel()
	defaults := SystemConfig{Proxy: ProxyConfig{HTTPProxy: "http://parent:1", NoProxy: "localhost"}}
	for _, tt := range []struct {
		name, yamlDoc string
		wantCleared   bool
	}{
		{"empty section", "proxy: {}\n", true},
		{"explicitly empty fields", "proxy:\n  httpProxy: \"\"\n  noProxy: \"\"\n", true},
		{"section absent", "hostname: child\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var child SystemConfig
			if err := yaml.Unmarshal([]byte(tt.yamlDoc), &child); err != nil {
				t.Fatal(err)
			}
			if tt.wantCleared && isEmptySystemConfig(child) {
				t.Fatal("an explicit proxy section must not be treated as an absent system config")
			}
			merged := mergeSystemConfig(defaults, child)
			if cleared := merged.Proxy.IsEmpty(); cleared != tt.wantCleared {
				t.Errorf("proxy after merge = %+v, cleared = %v, want %v", merged.Proxy, cleared, tt.wantCleared)
			}
		})
	}
}

func TestValidateAdditionalFilesNotGenerated(t *testing.T) {
	t.Parallel()
	proxy := ProxyConfig{HTTPProxy: "http://p:3128"}
	enabled := true
	cloudInit := CloudInitConfig{Enabled: &enabled}
	scripts := []ProvisioningScript{{Name: "s", Local: "/s.sh", Final: "/usr/local/sbin/s.sh"}}
	keyOnlyAdmin := []UserConfig{{Name: "admin", Sudo: true, SSHAuthorizedKeys: []string{testKey3}}}
	tests := []struct {
		name    string
		sc      SystemConfig
		file    AdditionalFileInfo
		wantErr bool
	}{
		{"proxy apt file", SystemConfig{Proxy: proxy},
			AdditionalFileInfo{Local: "/x/95ict-proxy", Final: "/etc/apt/apt.conf.d/95ict-proxy"}, true},
		{"proxy apt file via directory", SystemConfig{Proxy: proxy},
			AdditionalFileInfo{Local: "/x/95ict-proxy", Final: "/etc/apt/apt.conf.d/"}, true},
		{"proxy apt file with proxy unset", SystemConfig{},
			AdditionalFileInfo{Local: "/x/95ict-proxy", Final: "/etc/apt/apt.conf.d/95ict-proxy"}, false},
		{"apt file without a proxy URL", SystemConfig{Proxy: ProxyConfig{NoProxy: "localhost"}},
			AdditionalFileInfo{Local: "/x/95ict-proxy", Final: "/etc/apt/apt.conf.d/95ict-proxy"}, false},
		{"environment is merged", SystemConfig{Proxy: proxy},
			AdditionalFileInfo{Local: "/x/environment", Final: "/etc/environment"}, false},
		{"unrelated file in a generated directory", SystemConfig{Proxy: proxy},
			AdditionalFileInfo{Local: "/x/10-site.conf", Final: "/etc/apt/apt.conf.d/"}, false},
		{"cloud-init datasource", SystemConfig{CloudInit: cloudInit},
			AdditionalFileInfo{Local: "/x/ds", Final: "/etc/cloud/cloud.cfg.d/" + CloudInitDatasourceFile}, true},
		{"cloud-init datasource, cloud-init off", SystemConfig{},
			AdditionalFileInfo{Local: "/x/ds", Final: "/etc/cloud/cloud.cfg.d/" + CloudInitDatasourceFile}, false},
		{"under the stamp directory", SystemConfig{Provisioning: ProvisioningConfig{Scripts: scripts}},
			AdditionalFileInfo{Local: "/x/f", Final: ProvisioningStampDir + "/s"}, true},
		{"provisioning unit name", SystemConfig{Provisioning: ProvisioningConfig{Scripts: scripts}},
			AdditionalFileInfo{Local: "/x/f", Final: "/etc/systemd/system/ict-provision-000-s.service"}, true},
		{"key-only sudoers", SystemConfig{Users: keyOnlyAdmin},
			AdditionalFileInfo{Local: "/x/f", Final: KeyOnlySudoersFile}, true},
		{"sudoers with a password set", SystemConfig{Users: []UserConfig{{Name: "a", Sudo: true, Password: "x",
			SSHAuthorizedKeys: []string{testKey3}}}},
			AdditionalFileInfo{Local: "/x/f", Final: KeyOnlySudoersFile}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.sc.AdditionalFiles = []AdditionalFileInfo{tt.file}
			tmpl := &ImageTemplate{SystemConfig: tt.sc}
			if err := tmpl.validateAdditionalFilesNotGenerated(); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	immutable := &ImageTemplate{Target: TargetInfo{OS: "ubuntu"}, SystemConfig: SystemConfig{
		Immutability:    ImmutabilityConfig{Enabled: true},
		AdditionalFiles: []AdditionalFileInfo{{Local: "/x/f", Final: "/etc/apt/preferences.d/00-ict-apt-policy"}},
	}}
	if err := immutable.validateAdditionalFilesNotGenerated(); err == nil {
		t.Error("an additional file at the immutable apt policy path must be rejected")
	}
}

func TestApplyCompositionOverridesRechecksGeneratedPaths(t *testing.T) {
	t.Parallel()
	keyFile := writeProvisioningTestFile(t, filepath.Join(t.TempDir(), "k.pub"), testKey3+" cli\n")
	newTemplate := func() *ImageTemplate {
		return &ImageTemplate{SystemConfig: SystemConfig{
			Users: []UserConfig{{Name: "admin", Sudo: true}},
			AdditionalFiles: []AdditionalFileInfo{
				{Local: "/x/95ict-proxy", Final: "/etc/apt/apt.conf.d/95ict-proxy"},
				{Local: "/x/sudoers", Final: KeyOnlySudoersFile},
			},
		}}
	}
	if err := newTemplate().validateAdditionalFilesNotGenerated(); err != nil {
		t.Fatalf("template without a proxy or key-only user must load: %v", err)
	}
	for name, o := range map[string]CompositionOverrides{
		"proxy": {Proxy: ProxyOverrides{HTTPProxy: strPtr("http://p:3128")}},
		"key":   {SSHKeys: []SSHKeyOverride{{User: "admin", File: keyFile}}},
	} {
		o := o
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := newTemplate().ApplyCompositionOverrides(o); err == nil {
				t.Error("an override that enables a generated file over an additional file must be rejected")
			}
		})
	}
}
