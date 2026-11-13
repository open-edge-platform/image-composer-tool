package oepinstaller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// TestDownload is not parallel: it replaces the package's HTTP client.
func TestDownload(t *testing.T) {
	body := []byte("\"schema-version\": \"1\"\n")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sbom.yaml":
			_, _ = w.Write(body)
		case "/big":
			_, _ = w.Write(make([]byte, maxDownloadSize+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	orig := httpClient
	httpClient = srv.Client
	defer func() { httpClient = orig }()

	tests := []struct {
		name, path, sha, wantErr string
	}{
		{"matching digest", "/sbom.yaml", sha256Hex(body), ""},
		{"upper-case digest", "/sbom.yaml", strings.ToUpper(sha256Hex(body)), ""},
		{"digest mismatch", "/sbom.yaml", sha256Hex([]byte("other")), "SHA-256 is"},
		{"not found", "/missing", sha256Hex(body), "404"},
		{"too large", "/big", sha256Hex(body), "larger than"},
		{"no digest: not verified", "/sbom.yaml", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Download(srv.URL+tt.path, tt.sha)
			if tt.wantErr == "" {
				if err != nil || string(data) != string(body) {
					t.Fatalf("Download = %q, %v", data, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestNewDownloadRequestSendsTokenOnlyToGitHub(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url       string
		wantToken bool
	}{
		{"https://raw.githubusercontent.com/org/repo/abc/rendered/sbom.yaml", true},
		{"https://github.com/org/repo/releases/download/v1/openedge-cli", true},
		{"https://objects.githubusercontent.com/file", true},
		{"https://example.com/openedge-cli", false},
		{"https://github.com.example.com/openedge-cli", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()
			req, err := newDownloadRequest(context.Background(), tt.url, "secret")
			if err != nil {
				t.Fatal(err)
			}
			if got := req.Header.Get("Authorization") == "Bearer secret"; got != tt.wantToken {
				t.Errorf("token sent = %v, want %v", got, tt.wantToken)
			}
		})
	}
}

func TestDownloadHint(t *testing.T) {
	t.Parallel()
	noToken, _ := newDownloadRequest(context.Background(), "https://raw.githubusercontent.com/org/repo/abc/f", "")
	withToken, _ := newDownloadRequest(context.Background(), "https://raw.githubusercontent.com/org/repo/abc/f", "secret")
	other, _ := newDownloadRequest(context.Background(), "https://example.com/f", "")
	if hint := downloadHint(noToken, http.StatusNotFound); !strings.Contains(hint, TokenFileFlag) ||
		!strings.Contains(hint, TokenEnv) ||
		!strings.Contains(hint, TokenFileEnv) || !strings.Contains(hint, tokenFileConfig) ||
		!strings.Contains(hint, "private repository") {
		t.Errorf("hint without token = %q", hint)
	}
	if hint := downloadHint(withToken, http.StatusNotFound); !strings.Contains(hint, "read access") {
		t.Errorf("hint with token = %q", hint)
	}
	if hint := downloadHint(other, http.StatusNotFound); hint != "" {
		t.Errorf("hint for a non-GitHub host = %q", hint)
	}
	if hint := downloadHint(noToken, http.StatusInternalServerError); hint != "" {
		t.Errorf("hint for a server error = %q", hint)
	}
}

// TestResolveToken is not parallel: it sets environment variables and the
// global configuration.
func TestResolveToken(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	flagFile := write("flag-token", "from-flag\n", 0o600)
	envFile := write("env-token", "from-env-file\n", 0o600)
	cfgFile := write("cfg-token", "  from-config  \n", 0o600)
	empty := write("empty", " \n", 0o600)
	shared := write("shared", "readable-by-all", 0o644)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(cfgFile, link); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, flag, env, envFile, cfgFile, want, wantErr string
	}{
		{"nothing set", "", "", "", "", "", ""},
		{"build flag wins over the environment", flagFile, "from-env", envFile, cfgFile, "from-flag", ""},
		{"missing build flag file", filepath.Join(dir, "missing"), "from-env", "", "", "", TokenFileFlag},
		{"environment", "", "from-env", envFile, cfgFile, "from-env", ""},
		{"file from the environment", "", "", envFile, cfgFile, "from-env-file", ""},
		{"file from the configuration, trimmed", "", "", "", cfgFile, "from-config", ""},
		{"symlink to a file", "", "", link, "", "from-config", ""},
		{"readable by others still works", "", "", shared, "", "readable-by-all", ""},
		{"empty file", "", "", empty, "", "", "is empty"},
		{"missing file", "", "", filepath.Join(dir, "missing"), "", "", TokenFileEnv},
		{"missing configured file", "", "", "", filepath.Join(dir, "missing"), "", tokenFileConfig},
		{"directory", "", "", dir, "", "", "not a regular file"},
	}
	orig := *config.Global()
	t.Cleanup(func() { config.SetGlobal(&orig); SetTokenFile("") })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetTokenFile(tt.flag)
			t.Setenv(TokenEnv, tt.env)
			t.Setenv(TokenFileEnv, tt.envFile)
			cfg := orig
			cfg.OEPInstaller.TokenFile = tt.cfgFile
			config.SetGlobal(&cfg)
			got, err := resolveToken()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveToken() = %q, %v; want an error containing %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("resolveToken() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestCLIFile(t *testing.T) {
	t.Parallel()
	page := "https://github.com/org/oep-installer-cli/blob/main/rendered/openedge-cli-federal-aerospace"
	raw := "https://raw.githubusercontent.com/org/oep-installer-cli/c703dde/rendered/openedge-cli-federal-aerospace"
	other := "https://raw.githubusercontent.com/org/oep-installer-cli/c703dde/rendered/openedge-cli"
	if cliFile(page) != cliFile(raw) {
		t.Errorf("page %q and raw %q should name the same CLI file", cliFile(page), cliFile(raw))
	}
	if cliFile(raw) == cliFile(other) {
		t.Errorf("%q and %q should name different CLI files", raw, other)
	}
}

func TestCheckCLI(t *testing.T) {
	t.Parallel()
	const pinned = "https://raw.githubusercontent.com/org/oep-installer-cli/8d24c5b/rendered/" +
		"openedge-cli-federal-aerospace"
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name, cliURL, cliSHA256, templateURL, wantErr string
	}{
		{"same pinned CLI", pinned, "", pinned, ""},
		{"same pinned CLI and digest", pinned, digest, pinned, ""},
		{"same pinned CLI, other digest", pinned, strings.Repeat("f", 64), pinned, "SHA-256"},
		{"no cli-url", "", "", pinned, ""},
		{"branch page of the same file", "https://github.com/org/oep-installer-cli/blob/main/rendered/" +
			"openedge-cli-federal-aerospace", "", pinned, ""},
		{"pinned CLI at another commit", pinned, "",
			"https://raw.githubusercontent.com/org/oep-installer-cli/1234abc/rendered/openedge-cli-federal-aerospace",
			"a different version"},
		{"another CLI file", pinned, "",
			"https://raw.githubusercontent.com/org/oep-installer-cli/8d24c5b/rendered/openedge-cli", "a different CLI"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := &Output{CLIURL: tt.cliURL, CLISHA256: tt.cliSHA256}
			err := checkCLI(out, config.OEPInstallerConfig{URL: tt.templateURL, SHA256: digest})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestLoad is not parallel: it replaces the package's HTTP client.
func TestLoad(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "handheld_blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer srv.Close()
	orig := httpClient
	httpClient = srv.Client
	defer func() { httpClient = orig }()

	o := config.OEPInstallerConfig{SBOMURL: srv.URL + "/sbom.yaml", SBOMSHA256: sha256Hex(data),
		URL:     "https://raw.githubusercontent.com/example/oep-installer-cli/abc/rendered/openedge-cli-federal-aerospace",
		Modules: []string{"handheld_blueprint"}}
	out, err := Load(o)
	if err != nil || len(out.Packages) == 0 {
		t.Fatalf("Load = %+v, %v", out, err)
	}
	o.Modules = []string{"uncrewed_aerial_vehicle_blueprint"}
	if _, err := Load(o); err == nil || !strings.Contains(err.Error(), "module-list") {
		t.Fatalf("Load with other modules: err = %v", err)
	}
}

// TestResolve is not parallel: it replaces the package's HTTP client.
func TestResolve(t *testing.T) {
	cli := []byte("#!/bin/bash\necho openedge-cli\n")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(cli) }))
	defer srv.Close()
	orig := httpClient
	httpClient = srv.Client
	defer func() { httpClient = orig }()

	cliURL := srv.URL + "/rendered/openedge-cli-federal-aerospace"
	out := &Output{CLIURL: cliURL, CLISHA256: sha256Hex(cli), SHA256: "sbom-digest",
		Modules: []string{"handheld_blueprint"}, Arguments: []string{"--edgepack", "--docker"}}
	sbomOnly := config.OEPInstallerConfig{SBOMURL: "https://example.com/sbom.yaml", User: "admin"}

	got, data, err := Resolve(sbomOnly, out)
	if err != nil || string(data) != string(cli) {
		t.Fatalf("Resolve = %q, %v", data, err)
	}
	want := config.OEPInstallerConfig{URL: cliURL, SHA256: sha256Hex(cli), SBOMURL: sbomOnly.SBOMURL,
		SBOMSHA256: "sbom-digest", Modules: out.Modules, User: "admin", Args: out.Arguments}
	if got.URL != want.URL || got.SHA256 != want.SHA256 || got.SBOMSHA256 != want.SBOMSHA256 ||
		!slices.Equal(got.Modules, want.Modules) || !slices.Equal(got.Args, want.Args) || got.User != want.User {
		t.Errorf("Resolve = %+v, want %+v", got, want)
	}

	// Without cli-sha256 the CLI is still installed, unverified, and its digest recorded.
	unpinned := *out
	unpinned.CLISHA256 = ""
	if got, _, err := Resolve(sbomOnly, &unpinned); err != nil || got.SHA256 != sha256Hex(cli) {
		t.Errorf("Resolve without cli-sha256 = %+v, %v", got, err)
	}

	tests := []struct {
		name    string
		o       config.OEPInstallerConfig
		modify  func(*Output)
		wantErr string
	}{
		{"cli-sha256 mismatch", sbomOnly, func(o *Output) { o.CLISHA256 = strings.Repeat("0", 64) }, "SHA-256 is"},
		{"no cli-url", sbomOnly, func(o *Output) { o.CLIURL = "" }, "names no CLI"},
		{"http cli-url", sbomOnly, func(o *Output) { o.CLIURL = "http://example.com/cli" }, "not an https:// URL"},
		{"template CLI digest mismatch", config.OEPInstallerConfig{URL: cliURL, SHA256: strings.Repeat("0", 64)},
			func(*Output) {}, "SHA-256 is"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := *out
			tt.modify(&o)
			if _, _, err := Resolve(tt.o, &o); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	// A template that names its own CLI and modules keeps them.
	own := config.OEPInstallerConfig{URL: cliURL, SHA256: sha256Hex(cli), Modules: []string{"handheld_blueprint"},
		Args: []string{"--edgepack", "--docker"}}
	if got, _, err := Resolve(own, out); err != nil || got.URL != cliURL || !slices.Equal(got.Args, own.Args) {
		t.Errorf("Resolve with the template's CLI = %+v, %v", got, err)
	}
}
