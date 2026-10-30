package oepinstaller

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

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
			req, err := newDownloadRequest(tt.url, "secret")
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
	noToken, _ := newDownloadRequest("https://raw.githubusercontent.com/org/repo/abc/f", "")
	withToken, _ := newDownloadRequest("https://raw.githubusercontent.com/org/repo/abc/f", "secret")
	other, _ := newDownloadRequest("https://example.com/f", "")
	if hint := downloadHint(noToken, http.StatusNotFound); !strings.Contains(hint, TokenEnv) ||
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
	tests := []struct {
		name, cliURL, templateURL, wantErr string
	}{
		{"same pinned CLI", pinned, pinned, ""},
		{"no cli-url", "", pinned, ""},
		{"branch page of the same file", "https://github.com/org/oep-installer-cli/blob/main/rendered/" +
			"openedge-cli-federal-aerospace", pinned, ""},
		{"pinned CLI at another commit", pinned,
			"https://raw.githubusercontent.com/org/oep-installer-cli/1234abc/rendered/openedge-cli-federal-aerospace",
			"a different version"},
		{"another CLI file", pinned,
			"https://raw.githubusercontent.com/org/oep-installer-cli/8d24c5b/rendered/openedge-cli", "a different CLI"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkCLI(tt.cliURL, tt.templateURL)
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
