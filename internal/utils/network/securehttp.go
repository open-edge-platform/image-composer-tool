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
// Both constructors apply it so every caller enforces the guard consistently.
const responseHeaderTimeout = 30 * time.Second

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
		base.ResponseHeaderTimeout = responseHeaderTimeout
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
