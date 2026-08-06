package network

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// trustServerCert adds the httptest server's self-signed cert to the client's RootCAs.
func trustServerCert(t *testing.T, c *http.Client, ts *httptest.Server) {
	t.Helper()

	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.Transport)
	}
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	}

	// Extract the leaf certificate DER from the test server's TLS config.
	if len(ts.TLS.Certificates) == 0 || len(ts.TLS.Certificates[0].Certificate) == 0 {
		t.Fatalf("test server TLS has no certificate")
	}
	leafDER := ts.TLS.Certificates[0].Certificate[0]
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatalf("parsing test server certificate: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	tr.TLSClientConfig.RootCAs = pool
}

func TestNewSecureHTTPClient_TLSConfigBasics(t *testing.T) {
	c := NewSecureHTTPClient()
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.Transport)
	}
	if tr.TLSClientConfig == nil {
		t.Fatalf("expected non-nil TLSClientConfig")
	}
	cfg := tr.TLSClientConfig

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want TLS1.2 (%v)", cfg.MinVersion, tls.VersionTLS12)
	}
	if cfg.MaxVersion != tls.VersionTLS13 {
		t.Errorf("MaxVersion = %v, want TLS1.3 (%v)", cfg.MaxVersion, tls.VersionTLS13)
	}

	// Check the TLS 1.2 cipher filter contains the 256-bit GCM suites.
	want := map[uint16]bool{
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:   true,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384: true,
	}
	got := map[uint16]bool{}
	for _, cs := range cfg.CipherSuites {
		got[cs] = true
	}
	for cs := range want {
		if !got[cs] {
			t.Errorf("CipherSuites missing %v", cs)
		}
	}
}

// TestResponseHeaderTimeoutSet guards that both constructors set
// ResponseHeaderTimeout on their transport. Without these assertions a future
// constructor refactor could silently drop the timeout and reintroduce the
// indefinite header-phase hang this bounds.
func TestResponseHeaderTimeoutSet(t *testing.T) {
	once = sync.Once{}
	secureClient = nil

	for _, tc := range []struct {
		name   string
		client *http.Client
	}{
		{"NewSecureHTTPClient", NewSecureHTTPClient()},
		{"GetSecureHTTPClient", GetSecureHTTPClient()},
	} {
		tr, ok := tc.client.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("%s: expected *http.Transport, got %T", tc.name, tc.client.Transport)
		}
		if tr.ResponseHeaderTimeout != responseHeaderTimeout {
			t.Errorf("%s: ResponseHeaderTimeout = %v, want %v", tc.name, tr.ResponseHeaderTimeout, responseHeaderTimeout)
		}
	}
}

func TestNewSecureHTTPClient_ConnectsToTLS13Server(t *testing.T) {
	// TLS 1.3–only server
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.WriteString(w, "ok")
		if err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	ts.TLS = &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
	}
	ts.StartTLS()
	defer ts.Close()

	c := NewSecureHTTPClient()
	c.Timeout = 5 * time.Second

	// Trust the server's self-signed certificate.
	trustServerCert(t, c, ts)

	resp, err := c.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET %s failed: %v", ts.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.TLS == nil {
		t.Fatalf("resp.TLS was nil; expected TLS connection state")
	}
	if resp.TLS.Version != tls.VersionTLS13 {
		t.Errorf("negotiated TLS version = %v, want TLS1.3 (%v)", resp.TLS.Version, tls.VersionTLS13)
	}
}

func TestNewSecureHTTPClient_HTTP2EnabledByDefault(t *testing.T) {
	h2srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.WriteString(w, r.Proto)
		if err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	h2srv.EnableHTTP2 = true
	h2srv.StartTLS()
	defer h2srv.Close()

	c := NewSecureHTTPClient()
	c.Timeout = 5 * time.Second

	// Trust the server's self-signed certificate.
	trustServerCert(t, c, h2srv)

	resp, err := c.Get(h2srv.URL)
	if err != nil {
		t.Fatalf("GET %s failed: %v", h2srv.URL, err)
	}
	defer resp.Body.Close()

	if resp.ProtoMajor != 2 {
		t.Errorf("expected HTTP/2, got %s", resp.Proto)
	}
}

func TestGetSecureHTTPClient_Singleton(t *testing.T) {
	// Reset the singleton for this test
	once = sync.Once{}
	secureClient = nil

	c1 := GetSecureHTTPClient()
	c2 := GetSecureHTTPClient()

	if c1 != c2 {
		t.Errorf("GetSecureHTTPClient should return the same instance, got different instances")
	}

	// Verify it has the same TLS configuration as NewSecureHTTPClient
	tr, ok := c1.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c1.Transport)
	}
	if tr.TLSClientConfig == nil {
		t.Fatalf("expected non-nil TLSClientConfig")
	}
	cfg := tr.TLSClientConfig

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want TLS1.2 (%v)", cfg.MinVersion, tls.VersionTLS12)
	}
	if cfg.MaxVersion != tls.VersionTLS13 {
		t.Errorf("MaxVersion = %v, want TLS1.3 (%v)", cfg.MaxVersion, tls.VersionTLS13)
	}

	// Check the TLS 1.2 cipher filter contains the 256-bit GCM suites.
	want := map[uint16]bool{
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:   true,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384: true,
	}
	got := map[uint16]bool{}
	for _, cs := range cfg.CipherSuites {
		got[cs] = true
	}
	for cs := range want {
		if !got[cs] {
			t.Errorf("CipherSuites missing %v", cs)
		}
	}
}

func TestGetSecureHTTPClient_ConnectsToTLS13Server(t *testing.T) {
	// Reset the singleton for this test
	once = sync.Once{}
	secureClient = nil

	// TLS 1.3–only server
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.WriteString(w, "ok")
		if err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	ts.TLS = &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
	}
	ts.StartTLS()
	defer ts.Close()

	c := GetSecureHTTPClient()
	c.Timeout = 5 * time.Second

	// Trust the server's self-signed certificate.
	trustServerCert(t, c, ts)

	resp, err := c.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET %s failed: %v", ts.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.TLS == nil {
		t.Fatalf("resp.TLS was nil; expected TLS connection state")
	}
	if resp.TLS.Version != tls.VersionTLS13 {
		t.Errorf("negotiated TLS version = %v, want TLS1.3 (%v)", resp.TLS.Version, tls.VersionTLS13)
	}
}
