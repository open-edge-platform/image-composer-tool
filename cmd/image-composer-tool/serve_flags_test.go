// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/open-edge-platform/image-composer-tool/internal/api/auth"
)

// serveFlags snapshots the package-level serve flags and restores them when the
// test ends, so table cases can set them freely without leaking state.
func serveFlags(t *testing.T) {
	t.Helper()
	host, ex, url, tok, maxC := serveHost, serveExecutor, serveExecutorURL, serveExecutorToken, serveMaxConcurrent
	a, user, proxies, allow := serveAuth, serveAuthUser, serveAuthProxies, serveAuthAllowRemote
	t.Cleanup(func() {
		serveHost, serveExecutor, serveExecutorURL, serveExecutorToken, serveMaxConcurrent = host, ex, url, tok, maxC
		serveAuth, serveAuthUser, serveAuthProxies, serveAuthAllowRemote = a, user, proxies, allow
	})
}

func TestIdentityProvider(t *testing.T) {
	serveFlags(t)
	tests := []struct {
		name     string
		auth     string
		host     string
		proxies  string
		allow    bool
		wantMode string
		wantErr  string
	}{
		{name: "default is one local user", auth: "", host: "127.0.0.1", wantMode: "none"},
		{name: "trusted header on loopback", auth: "trusted-header", host: "127.0.0.1", wantMode: "trusted-header"},
		{name: "trusted header on localhost", auth: "trusted-header", host: "localhost", wantMode: "trusted-header"},
		{name: "trusted header on all interfaces is refused", auth: "trusted-header", host: "0.0.0.0",
			wantErr: "would accept identity headers from any peer"},
		{name: "named proxies allow a remote bind", auth: "trusted-header", host: "0.0.0.0",
			proxies: "10.0.0.5, 10.1.0.0/16", wantMode: "trusted-header"},
		{name: "explicit opt-in allows a remote bind", auth: "trusted-header", host: "0.0.0.0", allow: true,
			wantMode: "trusted-header"},
		{name: "bad proxy list", auth: "trusted-header", host: "127.0.0.1", proxies: "not-an-address",
			wantErr: "trusted proxy"},
		{name: "unknown mode", auth: "oidc", host: "127.0.0.1", wantErr: "unknown --auth"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			serveAuth, serveHost, serveAuthProxies, serveAuthAllowRemote = tc.auth, tc.host, tc.proxies, tc.allow
			p, err := identityProvider()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Mode() != tc.wantMode {
				t.Fatalf("mode = %q, want %q", p.Mode(), tc.wantMode)
			}
			if th, ok := p.(auth.TrustedHeader); ok && tc.proxies != "" && len(th.TrustedProxies) != 2 {
				t.Fatalf("trusted proxies = %v, want two networks", th.TrustedProxies)
			}
		})
	}
}

func TestBuildExecutor(t *testing.T) {
	serveFlags(t)
	service := func(protocol string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/info" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "farm", "protocol": protocol, "maxConcurrent": 4})
		}))
	}
	good := service("1")
	defer good.Close()
	old := service("0")
	defer old.Close()

	tests := []struct {
		name     string
		executor string
		url      string
		maxC     int
		wantNil  bool
		wantMax  int
		wantErr  string
	}{
		{name: "default is local with one slot", executor: "", wantNil: true, wantMax: 1},
		{name: "local refuses more than one slot", executor: "local", maxC: 2, wantErr: "--max-concurrent above 1"},
		{name: "remote needs a URL", executor: "remote", wantErr: "--executor-url"},
		{name: "remote takes the service's capacity", executor: "remote", url: good.URL, wantMax: 4},
		{name: "remote capacity can only be lowered", executor: "remote", url: good.URL, maxC: 2, wantMax: 2},
		{name: "remote ignores a higher --max-concurrent", executor: "remote", url: good.URL, maxC: 9, wantMax: 4},
		{name: "remote refuses another protocol", executor: "remote", url: old.URL, wantErr: "speaks protocol"},
		{name: "unknown executor", executor: "cloud", wantErr: "unknown --executor"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			serveExecutor, serveExecutorURL, serveMaxConcurrent = tc.executor, tc.url, tc.maxC
			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			ex, n, err := buildExecutor(cmd)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if (ex == nil) != tc.wantNil {
				t.Fatalf("executor = %v, want nil: %v", ex, tc.wantNil)
			}
			if n != tc.wantMax {
				t.Fatalf("max concurrent = %d, want %d", n, tc.wantMax)
			}
		})
	}
}
