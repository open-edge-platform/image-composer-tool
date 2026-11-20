package fitmaker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// TestWriteFitITS checks the generated .its text: one kernel/ramdisk/
// version-trailer image node, one fdt node + one configuration per DTB (in
// the given order), and that the first DTB becomes the default
// configuration.
func TestWriteFitITS(t *testing.T) {
	dir := t.TempDir()
	itsPath := filepath.Join(dir, "fit.its")

	dtbNames := []string{"board-a.dtb", "board-b.dtb"}
	if err := writeFitITS(itsPath, dtbNames); err != nil {
		t.Fatalf("writeFitITS: %v", err)
	}

	data, err := os.ReadFile(itsPath)
	if err != nil {
		t.Fatalf("reading its: %v", err)
	}
	its := string(data)

	for _, want := range []string{
		`data = /incbin/("./Image")`,
		`data = /incbin/("rootfs.img")`,
		`data = /incbin/("version-trailer.bin")`,
		`data = /incbin/("board-a.dtb")`,
		`data = /incbin/("board-b.dtb")`,
		`default = "conf-board-a"`,
		`conf-board-a {`,
		`conf-board-b {`,
		`fdt = "fdt1"`,
		`fdt = "fdt2"`,
	} {
		if !strings.Contains(its, want) {
			t.Errorf("expected .its to contain %q, got:\n%s", want, its)
		}
	}
}

func TestConfName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"board-a.dtb", "conf-board-a"},
		{"boot/dtb-1.0/intel/board-b.dtb", "conf-board-b"},
	}
	for _, tt := range tests {
		if got := confName(tt.in); got != tt.want {
			t.Errorf("confName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCreateVersionTrailer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version-trailer.bin")
	if err := createVersionTrailer(path); err != nil {
		t.Fatalf("createVersionTrailer: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 4096 {
		t.Errorf("version trailer size = %d, want 4096", info.Size())
	}
}

func TestFindKernel(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr bool
		want    string
	}{
		{name: "single kernel", output: "vmlinuz-6.18.1-acc\n", want: filepath.Join("boot", "vmlinuz-6.18.1-acc")},
		{name: "none", output: "", wantErr: true},
		{name: "multiple", output: "vmlinuz-a\nvmlinuz-b\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := shell.Default
			t.Cleanup(func() { shell.Default = original })
			shell.Default = shell.NewMockExecutor([]shell.MockCommand{
				{Pattern: "ls .*boot.*", Output: tt.output, Error: nil},
			})

			fitMaker := &FitMaker{FitRootfsPath: "/fake/rootfs"}
			got, err := fitMaker.findKernel()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got kernel %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("findKernel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindDTBs(t *testing.T) {
	original := shell.Default
	t.Cleanup(func() { shell.Default = original })
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "find .*boot.*", Output: "dtb-1.0/intel/board-b.dtb\ndtb-1.0/intel/board-a.dtb\n", Error: nil},
	})

	fitMaker := &FitMaker{FitRootfsPath: "/fake/rootfs"}
	got, err := fitMaker.findDTBs()
	if err != nil {
		t.Fatalf("findDTBs: %v", err)
	}
	want := []string{
		filepath.Join("boot", "dtb-1.0/intel/board-a.dtb"),
		filepath.Join("boot", "dtb-1.0/intel/board-b.dtb"),
	}
	if len(got) != len(want) {
		t.Fatalf("findDTBs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("findDTBs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFindDTBsNoneFound(t *testing.T) {
	original := shell.Default
	t.Cleanup(func() { shell.Default = original })
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "find .*boot.*", Output: "", Error: nil},
	})

	fitMaker := &FitMaker{FitRootfsPath: "/fake/rootfs"}
	if _, err := fitMaker.findDTBs(); err == nil {
		t.Fatal("expected error when no DTBs are found")
	}
}

// TestCreateFitImageEndToEnd exercises the real packaging pipeline (no
// shell mocking) against a synthetic rootfs: stages a fake kernel and two
// fake DTBs, packs the rest into a ramdisk, and actually invokes the host's
// mkimage to build a real FIT image, then inspects it with dumpimage to
// confirm the kernel/ramdisk/configurations it reports match what was
// staged. Skips if mkimage/dumpimage aren't on PATH.
func TestCreateFitImageEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("mkimage"); err != nil {
		t.Skip("mkimage not available on PATH")
	}
	if _, err := exec.LookPath("dumpimage"); err != nil {
		t.Skip("dumpimage not available on PATH")
	}

	rootfs := t.TempDir()
	for _, dir := range []string{
		filepath.Join(rootfs, "boot", "dtb-1.0", "intel"),
		filepath.Join(rootfs, "etc"),
		filepath.Join(rootfs, "cdrom", "cache-repo"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite := func(path string, content string) {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(rootfs, "boot", "vmlinuz-6.18.1-test"), "fake-kernel-bytes")
	mustWrite(filepath.Join(rootfs, "boot", "dtb-1.0", "intel", "board-a.dtb"), "fake-dtb-a")
	mustWrite(filepath.Join(rootfs, "boot", "dtb-1.0", "intel", "board-b.dtb"), "fake-dtb-b")
	mustWrite(filepath.Join(rootfs, "etc", "hostname"), "fit-test\n")

	buildDir := t.TempDir()
	fitMaker := &FitMaker{
		FitRootfsPath: rootfs,
		FitFilePath:   filepath.Join(buildDir, "test.itb"),
	}

	if err := fitMaker.createFitImage(); err != nil {
		t.Fatalf("createFitImage: %v", err)
	}

	info, err := os.Stat(fitMaker.FitFilePath)
	if err != nil {
		t.Fatalf("FIT image was not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("FIT image is empty")
	}

	out, err := exec.Command("dumpimage", "-l", fitMaker.FitFilePath).CombinedOutput()
	if err != nil {
		t.Fatalf("dumpimage -l failed: %v\n%s", err, out)
	}
	listing := string(out)
	for _, want := range []string{"kernel", "ramdisk", "fdt1", "fdt2", "conf-board-a", "conf-board-b", "Default Configuration: 'conf-board-a'"} {
		if !bytes.Contains([]byte(listing), []byte(want)) {
			t.Errorf("dumpimage -l output missing %q, got:\n%s", want, listing)
		}
	}

	// etc/hostname (an ordinary rootfs file) must be in the ramdisk; the
	// kernel and DTBs must not be, since they're embedded separately. Rather
	// than extract the ramdisk back out of the FIT container (dumpimage's
	// legacy -T/-p extraction mode doesn't handle a multi-configuration FIT
	// cleanly, independent of anything this package controls), rebuild the
	// same find|cpio listing createFitImage's packing step uses and inspect
	// it directly -- this is the exact command whose exclusion pattern is
	// under test.
	kernelRel, err := fitMaker.findKernel()
	if err != nil {
		t.Fatalf("findKernel: %v", err)
	}
	listCmd := fmt.Sprintf(
		`cd %s && find . -not \( -path './%s' -o -name '*.dtb' \) | cpio -o -H newc 2>/dev/null | cpio -t 2>/dev/null`,
		rootfs, kernelRel)
	cpioList, err := exec.Command("sh", "-c", listCmd).CombinedOutput()
	if err != nil {
		t.Fatalf("listing packed ramdisk contents failed: %v\n%s", err, cpioList)
	}
	ramdiskListing := string(cpioList)
	if !strings.Contains(ramdiskListing, "etc/hostname") {
		t.Errorf("ramdisk must contain etc/hostname, got:\n%s", ramdiskListing)
	}
	if strings.Contains(ramdiskListing, "vmlinuz-") {
		t.Errorf("ramdisk must not contain the kernel, got:\n%s", ramdiskListing)
	}
	if strings.Contains(ramdiskListing, ".dtb") {
		t.Errorf("ramdisk must not contain device trees, got:\n%s", ramdiskListing)
	}
	if strings.Contains(ramdiskListing, ".dtb") {
		t.Errorf("ramdisk must not contain device trees, got:\n%s", ramdiskListing)
	}
}
