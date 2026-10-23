package rpmutils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestExtractPrimaryReferenceFromRepomdData(t *testing.T) {
	tests := []struct {
		name    string
		repomd  string
		want    rpmPrimaryReference
		wantErr string
	}{
		{
			name:   "href only",
			repomd: `<repomd><data type="primary"><location href="repodata/p.xml.gz"/></data></repomd>`,
			want:   rpmPrimaryReference{Href: "repodata/p.xml.gz"},
		},
		{
			name: "integrity fields",
			repomd: `<repomd>
  <data type="filelists"><location href="repodata/f.xml.gz"/></data>
  <data type="primary">
    <checksum type="sha256">abc</checksum>
    <open-checksum type="sha256">def</open-checksum>
    <location href="repodata/p.xml.gz"/>
    <size>10</size>
    <open-size>20</open-size>
  </data>
</repomd>`,
			want: rpmPrimaryReference{
				Href: "repodata/p.xml.gz", ChecksumType: "SHA256", Checksum: "abc",
				Size: 10, OpenChecksumType: "SHA256", OpenChecksum: "def", OpenSize: 20,
			},
		},
		{
			name:    "checksum without type",
			repomd:  `<repomd><data type="primary"><checksum>abc</checksum><location href="p.xml.gz"/></data></repomd>`,
			wantErr: "partial checksum",
		},
		{
			name:    "open-checksum without value",
			repomd:  `<repomd><data type="primary"><open-checksum type="sha256"></open-checksum><location href="p.xml.gz"/></data></repomd>`,
			wantErr: "partial open-checksum",
		},
		{
			name:    "no primary entry",
			repomd:  `<repomd><data type="filelists"><location href="f.xml.gz"/></data></repomd>`,
			wantErr: "primary location not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractPrimaryReferenceFromRepomdData([]byte(tt.repomd))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("reference = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestVerifyRPMPrimaryBytes(t *testing.T) {
	data := []byte("primary metadata")
	good := primaryRefForData("repodata/p.xml.gz", data)

	tests := []struct {
		name    string
		primary rpmPrimaryReference
		wantErr string
	}{
		{name: "no integrity data", primary: rpmPrimaryReference{Href: "p.xml.gz"}},
		{name: "matching size and checksum", primary: good},
		{name: "size mismatch", primary: rpmPrimaryReference{Href: "p.xml.gz", Size: 1}, wantErr: "size mismatch"},
		{
			name:    "checksum mismatch",
			primary: rpmPrimaryReference{Href: "p.xml.gz", ChecksumType: "SHA256", Checksum: "00"},
			wantErr: "checksum mismatch",
		},
		{
			name:    "unsupported checksum type",
			primary: rpmPrimaryReference{Href: "p.xml.gz", ChecksumType: "MD5", Checksum: "00"},
			wantErr: "unsupported primary metadata checksum type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyRPMPrimaryBytes(data, tt.primary)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseRepositoryMetadata_RejectsDownloadChecksumMismatch(t *testing.T) {
	setRPMMetadataTestCache(t)

	primaryHref := "repodata/primary.xml.gz"
	published := compressGzip(t, testPrimaryXML("bash"))
	served := compressGzip(t, testPrimaryXML("curl"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(served)
	}))
	defer server.Close()

	cacheDir, err := rpmMetadataCacheDir(server.URL)
	if err != nil {
		t.Fatalf("rpmMetadataCacheDir() error = %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	repomd := testRepomdWithPrimary(primaryRefForData(primaryHref, published))
	if err := writeFileAtomic(filepath.Join(cacheDir, "repomd.xml"), repomd, 0644); err != nil {
		t.Fatalf("failed to seed repomd cache: %v", err)
	}

	_, err = ParseRepositoryMetadata(server.URL, primaryHref, nil)
	if err == nil || !strings.Contains(err.Error(), "verify downloaded RPM primary metadata") {
		t.Fatalf("error = %v, want download verification failure", err)
	}
}

func TestParseRepositoryMetadata_VerifiesDownloadForTrailingSlashRepoURL(t *testing.T) {
	setRPMMetadataTestCache(t)

	primaryHref := "repodata/primary.xml.gz"
	published := compressGzip(t, testPrimaryXML("bash"))
	served := compressGzip(t, testPrimaryXML("curl"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repodata/repomd.xml":
			_, _ = w.Write(testRepomdWithPrimary(primaryRefForData(primaryHref, published)))
		default:
			_, _ = w.Write(served)
		}
	}))
	defer server.Close()

	// UserPackages resolves the primary reference from the slash-stripped URL
	// but hands the repository URL, slash included, to ParseRepositoryMetadata.
	href, err := FetchPrimaryURL(GetRepoMetaDataURL(server.URL+"/", "repodata/repomd.xml"))
	if err != nil {
		t.Fatalf("FetchPrimaryURL() error = %v", err)
	}

	_, err = ParseRepositoryMetadata(server.URL+"/", href, nil)
	if err == nil || !strings.Contains(err.Error(), "verify downloaded RPM primary metadata") {
		t.Fatalf("error = %v, want download verification failure", err)
	}
}

func TestParseRepositoryMetadata_IgnoresIntegrityOfDifferentCachedPrimary(t *testing.T) {
	setRPMMetadataTestCache(t)

	oldHref := "repodata/old-primary.xml.gz"
	oldData := compressGzip(t, testPrimaryXML("bash"))
	newPrimary := primaryRefForData("repodata/new-primary.xml.gz", compressGzip(t, testPrimaryXML("curl")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(oldData)
	}))
	defer server.Close()

	cacheDir, err := rpmMetadataCacheDir(server.URL)
	if err != nil {
		t.Fatalf("rpmMetadataCacheDir() error = %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	if err := writeFileAtomic(filepath.Join(cacheDir, "repomd.xml"), testRepomdWithPrimary(newPrimary), 0644); err != nil {
		t.Fatalf("failed to seed repomd cache: %v", err)
	}

	pkgs, err := ParseRepositoryMetadata(server.URL, oldHref, nil)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata() should not check %s against another file's integrity data: %v", oldHref, err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Fatalf("packages = %+v, want bash", pkgs)
	}
}

func TestCachedPrimaryReferenceFor(t *testing.T) {
	href := "repodata/primary.xml.gz"
	other := "repodata/other.xml.gz"
	repomdRef := rpmPrimaryReference{Href: href, ChecksumType: "SHA256", Checksum: "aaa"}
	locationRef := rpmPrimaryReference{Href: href, ChecksumType: "SHA256", Checksum: "bbb"}

	tests := []struct {
		name     string
		repomd   *rpmPrimaryReference
		location *rpmPrimaryReference
		want     rpmPrimaryReference
	}{
		{name: "repomd wins over location cache", repomd: &repomdRef, location: &locationRef, want: repomdRef},
		{name: "location cache when repomd is gone", location: &locationRef, want: locationRef},
		{
			name:   "repomd publishing a leading-slash href",
			repomd: &rpmPrimaryReference{Href: "/" + href, ChecksumType: "SHA256", Checksum: "aaa"},
			want:   repomdRef,
		},
		{
			name:     "location cache holding a leading-slash href",
			location: &rpmPrimaryReference{Href: "/" + href, ChecksumType: "SHA256", Checksum: "bbb"},
			want:     locationRef,
		},
		{
			name:     "repomd for another href falls back to location cache",
			repomd:   &rpmPrimaryReference{Href: other, ChecksumType: "SHA256", Checksum: "ccc"},
			location: &locationRef,
			want:     locationRef,
		},
		{
			name:     "nothing describes the href",
			repomd:   &rpmPrimaryReference{Href: other, ChecksumType: "SHA256", Checksum: "ccc"},
			location: &rpmPrimaryReference{Href: other, ChecksumType: "SHA256", Checksum: "ddd"},
			want:     rpmPrimaryReference{Href: href},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.repomd != nil {
				if err := os.WriteFile(filepath.Join(dir, "repomd.xml"), testRepomdWithPrimary(*tt.repomd), 0644); err != nil {
					t.Fatalf("failed to write repomd.xml: %v", err)
				}
			}
			if tt.location != nil {
				if err := saveRPMPrimaryLocationCache(filepath.Join(dir, "primary.location.json"), *tt.location); err != nil {
					t.Fatalf("failed to write location cache: %v", err)
				}
			}
			if got := cachedPrimaryReferenceFor(dir, href); got != tt.want {
				t.Fatalf("cachedPrimaryReferenceFor() = %+v, want %+v", got, tt.want)
			}
		})
	}
	if got := cachedPrimaryReferenceFor("", href); got != (rpmPrimaryReference{Href: href}) {
		t.Fatalf("cachedPrimaryReferenceFor() without a cache dir = %+v, want href only", got)
	}
}

func TestParseRepositoryMetadata_ReusesCacheWrittenForTrailingSlashURL(t *testing.T) {
	cacheRoot := setRPMMetadataTestCache(t)

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// A build before the directory name was normalized cached the raw primary
	// metadata under a directory derived from the URL with its trailing slash.
	repoURL := server.URL + "/"
	primaryHref := "repodata/primary.xml.gz"
	legacyDir := filepath.Join(cacheRoot, "rpm-metadata", rpmMetadataDirName(repoURL))
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("failed to create legacy cache dir: %v", err)
	}
	saveOriginalXML(legacyDir, primaryHref, server.URL+"/"+primaryHref, compressGzip(t, testPrimaryXML("bash")))

	pkgs, err := ParseRepositoryMetadata(repoURL, primaryHref, nil)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata() should reuse the cache written for the slash-inclusive URL: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Fatalf("packages = %+v, want bash", pkgs)
	}
	if n := atomic.LoadInt32(&requests); n != 0 {
		t.Fatalf("HTTP requests = %d, want 0", n)
	}
}

func TestMigrateLegacyRPMMetadataDir_MovesOnceAndKeepsPermissions(t *testing.T) {
	root := t.TempDir()
	repoURL := "https://example.invalid/repo/"
	cacheDir := filepath.Join(root, rpmMetadataDirName(strings.TrimRight(repoURL, "/")))
	legacyDir := filepath.Join(root, rpmMetadataDirName(repoURL))
	for _, dir := range []string{cacheDir, legacyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string]os.FileMode{"primary.parsed.json": 0600, "primary.xml_aaaaaaaa_2000-01-01_00-00-00.gz": 0644}
	for name, mode := range files {
		if err := os.WriteFile(filepath.Join(legacyDir, name), []byte(name), mode); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		if err := os.Chmod(filepath.Join(legacyDir, name), mode); err != nil {
			t.Fatalf("chmod %s: %v", name, err)
		}
	}
	// A file the normalized directory already has is not overwritten.
	existing := filepath.Join(cacheDir, "repomd.xml")
	if err := os.WriteFile(existing, []byte("current"), 0644); err != nil {
		t.Fatalf("seed %s: %v", existing, err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "repomd.xml"), []byte("old"), 0644); err != nil {
		t.Fatalf("seed legacy repomd.xml: %v", err)
	}

	migrateLegacyRPMMetadataDir(repoURL, cacheDir)

	for name, mode := range files {
		info, err := os.Stat(filepath.Join(cacheDir, name))
		if err != nil {
			t.Fatalf("%s was not migrated: %v", name, err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode = %v, want %v", name, info.Mode().Perm(), mode)
		}
	}
	if data, _ := os.ReadFile(existing); string(data) != "current" {
		t.Fatalf("existing file was overwritten with %q", data)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy directory should be retired once migrated (err=%v)", err)
	}

	// Clearing the cache must not bring the old index back.
	if err := os.Remove(filepath.Join(cacheDir, "primary.parsed.json")); err != nil {
		t.Fatalf("failed to clear the parsed index: %v", err)
	}
	migrateLegacyRPMMetadataDir(repoURL, cacheDir)
	if _, err := os.Stat(filepath.Join(cacheDir, "primary.parsed.json")); !os.IsNotExist(err) {
		t.Fatalf("a cleared parsed index was restored from the legacy directory (err=%v)", err)
	}
}

// Another build sharing the cache can publish a file between the migration
// looking at the target and publishing to it; the newer file must survive.
func TestMigrateLegacyRPMMetadataDir_KeepsFileWrittenDuringMigration(t *testing.T) {
	root := t.TempDir()
	repoURL := "https://example.invalid/repo/"
	cacheDir := filepath.Join(root, rpmMetadataDirName(strings.TrimRight(repoURL, "/")))
	legacyDir := filepath.Join(root, rpmMetadataDirName(repoURL))
	for _, dir := range []string{cacheDir, legacyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for _, name := range []string{"repomd.xml", "primary.parsed.json"} {
		if err := os.WriteFile(filepath.Join(legacyDir, name), []byte("legacy"), 0644); err != nil {
			t.Fatalf("seed legacy %s: %v", name, err)
		}
	}

	beforeLegacyMigrationPublish = func(target string) {
		if err := os.WriteFile(target, []byte("fresh"), 0644); err != nil {
			t.Errorf("concurrent writer: %v", err)
		}
	}
	t.Cleanup(func() { beforeLegacyMigrationPublish = func(string) {} })

	migrateLegacyRPMMetadataDir(repoURL, cacheDir)

	for _, name := range []string{"repomd.xml", "primary.parsed.json"} {
		if data, _ := os.ReadFile(filepath.Join(cacheDir, name)); string(data) != "fresh" {
			t.Fatalf("%s = %q, want the concurrently written file to survive", name, data)
		}
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy directory should be retired (err=%v)", err)
	}
}

func TestCopyFileExclusive(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("payload"), 0600); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	target := filepath.Join(dir, "target")
	if err := copyFileExclusive(source, target); err != nil {
		t.Fatalf("copyFileExclusive() error = %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "payload" {
		t.Fatalf("target = %q, %v, want the copied payload", got, err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("target mode = %v, %v, want 0600", info.Mode().Perm(), err)
	}

	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("newer"), 0644); err != nil {
		t.Fatalf("failed to write existing target: %v", err)
	}
	if err := copyFileExclusive(source, existing); !os.IsExist(err) {
		t.Fatalf("copyFileExclusive() over an existing file error = %v, want one satisfying os.IsExist", err)
	}
	if got, _ := os.ReadFile(existing); string(got) != "newer" {
		t.Fatalf("existing file = %q, want it left untouched", got)
	}
}

func TestExtractPrimaryReference_RejectsMalformedSizes(t *testing.T) {
	for _, tt := range []struct{ name, element string }{
		{name: "non-numeric size", element: "<size>abc</size>"},
		{name: "negative size", element: "<size>-1</size>"},
		{name: "empty size", element: "<size></size>"},
		{name: "zero size", element: "<size>0</size>"},
		{name: "non-numeric open-size", element: "<open-size>abc</open-size>"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repomd := `<repomd><data type="primary"><location href="repodata/primary.xml.gz"/>` + tt.element + `</data></repomd>`
			if _, err := extractPrimaryReferenceFromRepomdData([]byte(repomd)); err == nil || !strings.Contains(err.Error(), "malformed repomd") {
				t.Fatalf("error = %v, want a malformed repomd error", err)
			}
		})
	}
}

func TestParseRepositoryMetadata_VerifiesDownloadForLeadingSlashHref(t *testing.T) {
	setRPMMetadataTestCache(t)

	primaryHref := "repodata/primary.xml.gz"
	published := compressGzip(t, testPrimaryXML("bash"))
	served := compressGzip(t, testPrimaryXML("curl"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(served)
	}))
	defer server.Close()

	cacheDir, err := rpmMetadataCacheDir(server.URL)
	if err != nil {
		t.Fatalf("rpmMetadataCacheDir() error = %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	repomd := testRepomdWithPrimary(primaryRefForData(primaryHref, published))
	if err := writeFileAtomic(filepath.Join(cacheDir, "repomd.xml"), repomd, 0644); err != nil {
		t.Fatalf("failed to seed repomd cache: %v", err)
	}

	_, err = ParseRepositoryMetadata(server.URL, "/"+primaryHref, nil)
	if err == nil || !strings.Contains(err.Error(), "verify downloaded RPM primary metadata") {
		t.Fatalf("error = %v, want download verification failure for a leading-slash href", err)
	}
}

// A copy killed part-way must not leave a truncated file under the final name:
// the next migration would take it for a finished one and retire the intact
// legacy source.
func TestMigrateLegacyRPMMetadataDir_RetriesInterruptedCopy(t *testing.T) {
	root := t.TempDir()
	repoURL := "https://example.invalid/repo/"
	cacheDir := filepath.Join(root, rpmMetadataDirName(strings.TrimRight(repoURL, "/")))
	legacyDir := filepath.Join(root, rpmMetadataDirName(repoURL))
	for _, dir := range []string{cacheDir, legacyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	source := filepath.Join(legacyDir, "repomd.xml")
	target := filepath.Join(cacheDir, "repomd.xml")
	if err := os.WriteFile(source, []byte("legacy payload"), 0644); err != nil {
		t.Fatalf("seed legacy repomd.xml: %v", err)
	}

	// No hard links, so the migration has to copy.
	linkFile = func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EPERM}
	}
	t.Cleanup(func() {
		linkFile = os.Link
		beforeCopyPublish = func(string, string) {}
	})

	beforeCopyPublish = func(tmpPath, finalPath string) {
		if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
			t.Errorf("target visible before the copy was published (err=%v)", err)
		}
		panic("simulated kill during copy")
	}
	func() {
		defer func() { _ = recover() }()
		migrateLegacyRPMMetadataDir(repoURL, cacheDir)
	}()

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("interrupted copy left %s behind (err=%v)", target, err)
	}
	if data, err := os.ReadFile(source); err != nil || string(data) != "legacy payload" {
		t.Fatalf("legacy source = %q, %v, want it intact", data, err)
	}

	beforeCopyPublish = func(string, string) {}
	migrateLegacyRPMMetadataDir(repoURL, cacheDir)

	if data, err := os.ReadFile(target); err != nil || string(data) != "legacy payload" {
		t.Fatalf("migrated file = %q, %v, want the full payload", data, err)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy directory should be retired after the retry (err=%v)", err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(cacheDir, legacyCopyTempPrefix+"*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary copies left behind: %v", leftovers)
	}
}

func TestCopyFileExclusive_KeepsFileWrittenBeforeFallbackRename(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(source, []byte("payload"), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}
	linkFile = func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EPERM}
	}
	t.Cleanup(func() {
		linkFile = os.Link
		beforeCopyPublish = func(string, string) {}
	})
	beforeCopyPublish = func(_, finalPath string) {
		if err := os.WriteFile(finalPath, []byte("fresh"), 0644); err != nil {
			t.Errorf("concurrent writer: %v", err)
		}
	}

	if err := copyFileExclusive(source, target); !os.IsExist(err) {
		t.Fatalf("copyFileExclusive() error = %v, want one satisfying os.IsExist", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "fresh" {
		t.Fatalf("target = %q, want the concurrently written file kept", got)
	}
}

func TestMigrateLegacyRPMMetadataDir_KeepsLegacyWhenNoSafePublication(t *testing.T) {
	root := t.TempDir()
	repoURL := "https://example.invalid/repo/"
	cacheDir := filepath.Join(root, rpmMetadataDirName(strings.TrimRight(repoURL, "/")))
	legacyDir := filepath.Join(root, rpmMetadataDirName(repoURL))
	for _, dir := range []string{cacheDir, legacyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	source := filepath.Join(legacyDir, "repomd.xml")
	target := filepath.Join(cacheDir, "repomd.xml")
	if err := os.WriteFile(source, []byte("legacy payload"), 0644); err != nil {
		t.Fatalf("seed legacy repomd.xml: %v", err)
	}
	linkFile = func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EPERM}
	}
	origRename := renameNoReplace
	renameNoReplace = func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EINVAL}
	}
	t.Cleanup(func() {
		linkFile = os.Link
		renameNoReplace = origRename
	})

	migrateLegacyRPMMetadataDir(repoURL, cacheDir)

	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("%s was published without a safe way to do it (err=%v)", target, err)
	}
	if data, err := os.ReadFile(source); err != nil || string(data) != "legacy payload" {
		t.Fatalf("legacy source = %q, %v, want it kept for a later run", data, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(cacheDir, legacyCopyTempPrefix+"*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary copies left behind: %v", leftovers)
	}
}
