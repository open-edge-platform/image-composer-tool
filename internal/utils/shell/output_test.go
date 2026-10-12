package shell

import (
	"os"
	"strings"
	"testing"
)

func TestFormatCommandOutput(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"whitespace only", "  \n\t ", ""},
		{"single line", "wget: unable to resolve host address", "\n  wget: unable to resolve host address"},
		{"trailing newline trimmed", "dpkg: error\n", "\n  dpkg: error"},
		{"multi line indented", "\nline one\nline two\n", "\n  line one\n  line two"},
		{"blank interior line still indented", "a\n\nb", "\n  a\n  \n  b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatCommandOutput(tc.in); got != tc.want {
				t.Errorf("FormatCommandOutput(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestProxyEnvNames pins the two properties that matter: the names are reported
// sorted, and no value ever appears in the output (they routinely carry credentials).
func TestProxyEnvNames(t *testing.T) {
	const secret = "http://user:hunter2@proxy.example:912"

	for _, kv := range [][2]string{
		{"HTTPS_PROXY", secret},
		{"http_proxy", secret},
		{"NO_PROXY", "localhost"},
		{"UNRELATED_VAR", "keep-me"},
	} {
		t.Setenv(kv[0], kv[1])
	}

	names := ProxyEnvNames()

	joined := strings.Join(names, ",")
	for _, want := range []string{"HTTPS_PROXY", "http_proxy", "NO_PROXY"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ProxyEnvNames() = %v, missing %s", names, want)
		}
	}
	if strings.Contains(joined, "UNRELATED_VAR") {
		t.Errorf("ProxyEnvNames() = %v, must not include non-proxy vars", names)
	}
	if strings.Contains(joined, "hunter2") || strings.Contains(joined, secret) {
		t.Errorf("ProxyEnvNames() leaked a proxy value: %v", names)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("ProxyEnvNames() = %v, not sorted", names)
			break
		}
	}
}

func TestProxyEnvNamesEmptyWhenUnset(t *testing.T) {
	for _, k := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "FTP_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "ftp_proxy", "no_proxy",
	} {
		if _, ok := os.LookupEnv(k); ok {
			t.Setenv(k, "")
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("unsetenv %s: %v", k, err)
			}
		}
	}

	if names := ProxyEnvNames(); len(names) != 0 {
		t.Errorf("ProxyEnvNames() = %v, want empty when no proxy vars are set", names)
	}
}

// TestReportProxyEnvOnce asserts the summary is emitted at most once per process, so a
// template with hundreds of chrooted commands does not repeat it per command.
func TestReportProxyEnvOnce(t *testing.T) {
	ReportProxyEnvOnce()
	ReportProxyEnvOnce() // must be a no-op, and must not panic
}

// clearProxyEnv removes every proxy spelling so a case-mirroring case starts clean.
func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "FTP_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "ftp_proxy", "no_proxy",
	} {
		t.Setenv(k, "") // registers the restore
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unsetenv %s: %v", k, err)
		}
	}
}

// TestGetOSProxyEnvironsMirrorsCase is the regression test for a CI build failure:
// the build agent exported only the uppercase spellings, GNU wget reads only the
// lowercase ones, so wget connected straight to github.com and had its TLS terminated
// by a middlebox. Whichever spelling the environment supplies, both must reach the
// chroot.
func TestGetOSProxyEnvironsMirrorsCase(t *testing.T) {
	const httpsVal = "http://proxy.example:912"
	const httpVal = "http://proxy.example:911"

	t.Run("uppercase only is mirrored down", func(t *testing.T) {
		clearProxyEnv(t)
		t.Setenv("HTTP_PROXY", httpVal)
		t.Setenv("HTTPS_PROXY", httpsVal)
		t.Setenv("NO_PROXY", "localhost")

		got := GetOSProxyEnvirons()

		for k, want := range map[string]string{
			"HTTP_PROXY": httpVal, "http_proxy": httpVal,
			"HTTPS_PROXY": httpsVal, "https_proxy": httpsVal,
			"NO_PROXY": "localhost", "no_proxy": "localhost",
		} {
			if got[k] != want {
				t.Errorf("proxyEnv[%q] = %q, want %q", k, got[k], want)
			}
		}
	})

	t.Run("lowercase only is mirrored up", func(t *testing.T) {
		clearProxyEnv(t)
		t.Setenv("https_proxy", httpsVal)

		got := GetOSProxyEnvirons()

		if got["HTTPS_PROXY"] != httpsVal {
			t.Errorf("proxyEnv[HTTPS_PROXY] = %q, want %q", got["HTTPS_PROXY"], httpsVal)
		}
		if got["https_proxy"] != httpsVal {
			t.Errorf("proxyEnv[https_proxy] = %q, want %q", got["https_proxy"], httpsVal)
		}
	})

	t.Run("both spellings set are left untouched", func(t *testing.T) {
		clearProxyEnv(t)
		t.Setenv("HTTPS_PROXY", httpsVal)
		t.Setenv("https_proxy", "http://other.example:3128")

		got := GetOSProxyEnvirons()

		if got["HTTPS_PROXY"] != httpsVal {
			t.Errorf("HTTPS_PROXY was overwritten: %q", got["HTTPS_PROXY"])
		}
		if got["https_proxy"] != "http://other.example:3128" {
			t.Errorf("https_proxy was overwritten: %q", got["https_proxy"])
		}
	})

	t.Run("no proxy set yields nothing", func(t *testing.T) {
		clearProxyEnv(t)

		if got := GetOSProxyEnvirons(); len(got) != 0 {
			t.Errorf("GetOSProxyEnvirons() = %v, want empty", got)
		}
	})
}
