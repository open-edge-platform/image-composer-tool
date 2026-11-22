package deb

import (
	"fmt"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestValidateCrossArchDepsEmulatorVariants(t *testing.T) {
	originalExecutor := shell.Default
	originalArch := runtimeGOARCH
	defer func() {
		shell.Default = originalExecutor
		runtimeGOARCH = originalArch
	}()

	// Commands are anchored so "qemu-aarch64" does not also match
	// "qemu-aarch64-static".
	present := func(cmd string) shell.MockCommand {
		return shell.MockCommand{Pattern: "^command -v " + cmd + "$", Output: "/usr/bin/" + cmd, Error: nil}
	}
	absent := func(cmd string) shell.MockCommand {
		return shell.MockCommand{Pattern: "^command -v " + cmd + "$", Output: "", Error: fmt.Errorf("exit status 1")}
	}
	// system.QemuUserPackage runs these to name the package in the error hint.
	aptMocks := []shell.MockCommand{
		present("apt-get"),
		{Pattern: "^apt-get -s install qemu-user-static$", Output: "", Error: nil},
	}

	tests := []struct {
		name     string
		hostArch string // runtime.GOARCH value
		target   string // Debian architecture name
		static   string
		dynamic  string
	}{
		{name: "amd64 host, arm64 target", hostArch: "amd64", target: "arm64", static: "qemu-aarch64-static", dynamic: "qemu-aarch64"},
		{name: "arm64 host, amd64 target", hostArch: "arm64", target: "amd64", static: "qemu-x86_64-static", dynamic: "qemu-x86_64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeGOARCH = tt.hostArch
			common := []shell.MockCommand{present("arch-test"), present("update-binfmts")}

			cases := []struct {
				name    string
				mocks   []shell.MockCommand
				wantErr string // empty means success
			}{
				{name: "static emulator only (qemu-user-static)", mocks: []shell.MockCommand{present(tt.static), absent(tt.dynamic)}},
				{name: "dynamic emulator only (qemu-user-binfmt)", mocks: []shell.MockCommand{absent(tt.static), present(tt.dynamic)}},
				{name: "both emulators present", mocks: []shell.MockCommand{present(tt.static), present(tt.dynamic)}},
				{name: "no emulator", mocks: []shell.MockCommand{absent(tt.static), absent(tt.dynamic)}, wantErr: tt.static},
			}
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					mocks := append(append(append([]shell.MockCommand{}, c.mocks...), common...), aptMocks...)
					shell.Default = shell.NewMockExecutor(mocks)

					err := NewDebInstaller().validateCrossArchDeps(tt.target)
					if c.wantErr == "" {
						if err != nil {
							t.Fatalf("expected the emulator check to pass, got: %v", err)
						}
						return
					}
					if err == nil {
						t.Fatal("expected an error when no emulator is installed, got nil")
					}
					if !strings.Contains(err.Error(), c.wantErr) || !strings.Contains(err.Error(), "qemu-user-static") {
						t.Fatalf("error should name %q and the package to install, got: %v", c.wantErr, err)
					}
				})
			}
		})
	}
}

func TestValidateCrossArchDepsSameArchSkipsCheck(t *testing.T) {
	originalArch := runtimeGOARCH
	defer func() { runtimeGOARCH = originalArch }()
	runtimeGOARCH = "amd64"
	if err := NewDebInstaller().validateCrossArchDeps("amd64"); err != nil {
		t.Fatalf("a native build needs no emulator, got: %v", err)
	}
}
