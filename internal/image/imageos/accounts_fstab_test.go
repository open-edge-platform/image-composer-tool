package imageos

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

var testSHA512Hash = "$6$saltsalt$" + strings.Repeat("aB0./", 17) + "x"

func TestSetUserPasswordPreHashedWithoutAlgo(t *testing.T) {
	orig := shell.Default
	t.Cleanup(func() { shell.Default = orig })
	// Only usermod -p is mocked: falling through to `passwd` (which would set
	// the hash as a literal password) makes the mock return an error.
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `usermod -p '` + regexp.QuoteMeta(testSHA512Hash) + `' admin`, Output: ""},
		{Pattern: `.*`, Error: errFake},
	})
	user := config.UserConfig{Name: "admin", Password: testSHA512Hash}
	if err := setUserPassword("/", user); err != nil {
		t.Fatalf("pre-hashed password without hash_algo must be set with usermod -p: %v", err)
	}
}

func TestSetUserPasswordIncompleteHashIsNotWrittenAsHash(t *testing.T) {
	orig := shell.Default
	t.Cleanup(func() { shell.Default = orig })
	// usermod -p would store "$6$salt" as an unusable shadow entry; it must go
	// through passwd like any other plaintext value.
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `usermod -p .*`, Error: errFake},
		{Pattern: `passwd admin`, Output: ""},
	})
	for _, algo := range []string{"", "sha512"} {
		user := config.UserConfig{Name: "admin", Password: "$6$salt", HashAlgo: algo}
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: `usermod -p '\$6\$hashedplain' admin`, Output: ""},
			{Pattern: `openssl passwd -6 .*`, Output: "$6$hashedplain"},
			{Pattern: `passwd admin`, Output: ""},
		})
		if err := setUserPassword("/", user); err != nil {
			t.Errorf("hash_algo %q: incomplete hash must be treated as plaintext: %v", algo, err)
		}
	}
}

func TestAddOrUpdateUserAccount(t *testing.T) {
	tests := []struct {
		name    string
		user    config.UserConfig
		mocks   []shell.MockCommand
		wantErr bool
	}{
		{
			name:  "default shell",
			user:  config.UserConfig{Name: "a"},
			mocks: []shell.MockCommand{{Pattern: `^.*useradd -m -s '/bin/bash' a$`}},
		},
		{
			name:  "custom shell and home",
			user:  config.UserConfig{Name: "a", Shell: "/bin/zsh", Home: "/srv/a"},
			mocks: []shell.MockCommand{{Pattern: `useradd -m -s '/bin/zsh' -d '/srv/a' a`}},
		},
		{
			name: "existing user gets shell via usermod",
			user: config.UserConfig{Name: "root", Shell: "/bin/zsh"},
			mocks: []shell.MockCommand{
				{Pattern: `useradd`, Output: "useradd: user 'root' already exists", Error: errFake},
				{Pattern: `usermod -s '/bin/zsh' root`},
			},
		},
		{
			name:    "useradd failure",
			user:    config.UserConfig{Name: "a"},
			mocks:   []shell.MockCommand{{Pattern: `useradd`, Output: "boom", Error: errFake}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := shell.Default
			t.Cleanup(func() { shell.Default = orig })
			shell.Default = shell.NewMockExecutor(append(tt.mocks, shell.MockCommand{Pattern: `.*`, Error: errFake}))
			if err := addOrUpdateUserAccount("/", tt.user); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFstabEntry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		mountPoint string
		partition  config.PartitionInfo
		want       string
	}{
		{"root", "/", config.PartitionInfo{FsType: "ext4"}, "PARTUUID=x / ext4 defaults 0 1\n"},
		{"esp", "/boot/efi", config.PartitionInfo{FsType: "fat32", MountOptions: "umask=0077"},
			"PARTUUID=x /boot/efi vfat umask=0077 0 2\n"},
		{"data", "/data", config.PartitionInfo{FsType: "ext4", MountOptions: "defaults,nofail"},
			"PARTUUID=x /data ext4 defaults,nofail 0 2\n"},
		{"swap", "", config.PartitionInfo{FsType: "linux-swap"}, "PARTUUID=x none swap sw 0 0\n"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fstabEntry("PARTUUID=x", tt.mountPoint, tt.partition); got != tt.want {
				t.Errorf("fstabEntry = %q, want %q", got, tt.want)
			}
		})
	}
}

// fstabExecutor serves blkid lookups and performs file.Append's
// "cat TMP | sudo tee -a DST" in Go; anything else fails rather than
// reaching the real shell.
type fstabExecutor struct{}

func (e fstabExecutor) run(cmd string) (string, error) {
	if strings.HasPrefix(cmd, "blkid /dev/") {
		dev := strings.Fields(cmd)[1]
		return "uuid-" + strings.TrimPrefix(dev, "/dev/"), nil
	}
	if strings.HasPrefix(cmd, "cat ") && strings.Contains(cmd, "| sudo tee -a ") {
		fields := strings.Fields(cmd)
		data, err := os.ReadFile(fields[1])
		if err != nil {
			return "", err
		}
		_, after, _ := strings.Cut(cmd, "tee -a ")
		dst := strings.Fields(after)[0]
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return "", err
		}
		f, err := os.OpenFile(dst, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return "", err
		}
		defer f.Close()
		_, err = f.Write(data)
		return "", err
	}
	return "", fmt.Errorf("unexpected command: %s", cmd)
}

func (e fstabExecutor) ExecCmd(c string, _ bool, _ string, _ []string) (string, error) {
	return e.run(c)
}
func (e fstabExecutor) ExecCmdSilent(c string, _ bool, _ string, _ []string) (string, error) {
	return e.run(c)
}
func (e fstabExecutor) ExecCmdWithStream(c string, _ bool, _ string, _ []string) (string, error) {
	return e.run(c)
}
func (e fstabExecutor) ExecCmdWithInput(_ string, c string, _ bool, _ string, _ []string) (string, error) {
	return e.run(c)
}
func (e fstabExecutor) ExecCmdSilentWithInput(_ string, c string, _ bool, _ string, _ []string) (string, error) {
	return e.run(c)
}

func TestUpdateImageFstabSkipsUnmountedAndKeepsOrder(t *testing.T) {
	orig := shell.Default
	t.Cleanup(func() { shell.Default = orig })
	shell.Default = fstabExecutor{}

	installRoot := t.TempDir()
	tmpl := &config.ImageTemplate{Disk: config.DiskConfig{Partitions: []config.PartitionInfo{
		{ID: "boot", FsType: "fat32", MountPoint: "/boot/efi"},
		{ID: "root", FsType: "ext4", MountPoint: "/"},
		{ID: "raw", FsType: "ext4"},
		{ID: "swap", FsType: "linux-swap"},
	}}}
	diskMap := map[string]string{"swap": "/dev/vda4", "raw": "/dev/vda3", "root": "/dev/vda2", "boot": "/dev/vda1"}
	if err := updateImageFstab(installRoot, diskMap, tmpl); err != nil {
		t.Fatalf("updateImageFstab: %v", err)
	}
	content := readFileForTest(t, filepath.Join(installRoot, "etc", "fstab"))
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 fstab lines (unmounted partition skipped), got:\n%s", content)
	}
	wantPrefixes := []string{"PARTUUID=uuid-vda1 /boot/efi", "PARTUUID=uuid-vda2 / ", "PARTUUID=uuid-vda4 none swap"}
	for i, prefix := range wantPrefixes {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Errorf("line %d = %q, want prefix %q", i, lines[i], prefix)
		}
	}
}

var errFake = errors.New("fake command failure")

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestClearUserPassword(t *testing.T) {
	tests := []struct {
		name    string
		user    config.UserConfig
		mocks   []shell.MockCommand
		wantErr bool
	}{
		{
			name:  "key-only account is locked",
			user:  config.UserConfig{Name: "admin", SSHAuthorizedKeys: []string{"ssh-ed25519 AAAA"}},
			mocks: []shell.MockCommand{{Pattern: `passwd -l admin`}},
		},
		{
			name:  "account without keys keeps the deleted password",
			user:  config.UserConfig{Name: "guest"},
			mocks: []shell.MockCommand{{Pattern: `passwd -d guest`}},
		},
		{
			name:    "lock failure",
			user:    config.UserConfig{Name: "admin", SSHAuthorizedKeys: []string{"ssh-ed25519 AAAA"}},
			mocks:   []shell.MockCommand{{Pattern: `passwd -l admin`, Error: errFake}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := shell.Default
			t.Cleanup(func() { shell.Default = orig })
			shell.Default = shell.NewMockExecutor(append(tt.mocks, shell.MockCommand{Pattern: `.*`, Error: errFake}))
			if err := clearUserPassword("/", tt.user); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetPasswordMaxAge(t *testing.T) {
	days := func(v int) *int { return &v }
	tests := []struct {
		name    string
		age     *int
		mocks   []shell.MockCommand
		wantErr bool
	}{
		// No command may run: the catch-all mock fails any call.
		{name: "omitted leaves the account default"},
		{name: "positive value is applied", age: days(90), mocks: []shell.MockCommand{{Pattern: `passwd -x 90 admin`}}},
		{name: "explicit zero removes the limit", age: days(0), mocks: []shell.MockCommand{{Pattern: `passwd -x -1 admin`}}},
		{name: "command failure", age: days(90), mocks: []shell.MockCommand{{Pattern: `passwd -x 90 admin`, Error: errFake}},
			wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := shell.Default
			t.Cleanup(func() { shell.Default = orig })
			shell.Default = shell.NewMockExecutor(append(tt.mocks, shell.MockCommand{Pattern: `.*`, Error: errFake}))
			err := setPasswordMaxAge("/", config.UserConfig{Name: "admin", PasswordMaxAge: tt.age})
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCreateUserRejectsSudoAccountWithoutCredential(t *testing.T) {
	orig := shell.Default
	t.Cleanup(func() { shell.Default = orig })
	// No command may run: the account is rejected before useradd.
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{{Pattern: `.*`, Error: errFake}})
	tmpl := &config.ImageTemplate{SystemConfig: config.SystemConfig{
		Users: []config.UserConfig{{Name: "admin", Sudo: true}},
	}}
	err := createUser("/", tmpl)
	if err == nil || !strings.Contains(err.Error(), "no password or SSH authorized key") {
		t.Fatalf("err = %v, want a missing-credential error", err)
	}
}
