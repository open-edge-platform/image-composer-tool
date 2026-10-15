package network

import (
	"crypto/tls"
	"net/http"
	"sync"
	"time"
)

var (
	secureClient *http.Client
	once         sync.Once
)

// responseHeaderTimeout bounds the wait for a response's headers after the
// request is written. It guards the "connection established but the server or
// proxy never answers" stall: without it the request can block indefinitely,
// because http.Client has no default read deadline once a connection is up.
// It does NOT bound the response body transfer — a large package download can
// legitimately take much longer than this — so mid-body stalls are handled
// separately by the caller wrapping the body read with an idle timeout.
// NewSecureHTTPClient applies it; GetSecureHTTPClient, the package-download
// client, applies downloadResponseHeaderTimeout instead.
const responseHeaderTimeout = 30 * time.Second

// downloadResponseHeaderTimeout is the header wait for package downloads. A
// TLS-inspecting egress proxy holds the headers until it has buffered and
// scanned the start of the file (all of it, for small ones): behind one such
// proxy that took 27-36s for 26-39 MB packages fetched one at a time. The
// default 8 download workers share that proxy, so 5 min (8 x 36s, rounded up)
// is deliberate. Only a server that accepts the request and then never answers
// pays for it (up to 3 x 5 min per package); an unreachable one still fails on
// the transport's 30s dial and 10s TLS handshake timeouts.
const downloadResponseHeaderTimeout = 5 * time.Minute

// GetSecureHTTPClient returns a singleton secure HTTP client
func GetSecureHTTPClient() *http.Client {
	once.Do(func() {
		base := http.DefaultTransport.(*http.Transport).Clone()
		base.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS13,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			},
		}
		base.ResponseHeaderTimeout = downloadResponseHeaderTimeout
		secureClient = &http.Client{Transport: base}
	})
	return secureClient
}

// NewSecureHTTPClient returns an http.Client with a custom TLS configuration.
func NewSecureHTTPClient() *http.Client {

	// Clone, to start from defaults and only override what is required
	base := http.DefaultTransport.(*http.Transport).Clone()

	// TLS policy
	base.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			// (intentionally omit non-allowed ciphers per Intel CT-35)
		},
	}
	base.ResponseHeaderTimeout = responseHeaderTimeout

	return &http.Client{Transport: base}
}
