// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoneIsOneFixedUser(t *testing.T) {
	t.Parallel()
	id, err := None{}.Identify(httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil))
	if err != nil || id.Owner() != "local" {
		t.Fatalf("None{} = %+v, %v", id, err)
	}
	id, _ = None{User: "alice"}.Identify(httptest.NewRequest(http.MethodGet, "/", nil))
	if id.Owner() != "alice" || id.DisplayName() != "alice" {
		t.Fatalf("None{alice} = %+v", id)
	}
}

func TestTrustedHeaderReadsTheProxyHeaders(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	r.Header.Set("X-Forwarded-User", "abc123")
	r.Header.Set("X-Forwarded-Preferred-Username", "Alice B")
	r.Header.Set("X-Forwarded-Email", "alice@example.com")
	r.Header.Set("X-Forwarded-Groups", "builders, architects ,")
	id, err := TrustedHeader{}.Identify(r)
	if err != nil {
		t.Fatal(err)
	}
	if id.Owner() != "abc123" || id.DisplayName() != "Alice B" || id.Email != "alice@example.com" || len(id.Groups) != 2 {
		t.Fatalf("identity = %+v", id)
	}
}

// Without the header the request must be refused, never attributed to a
// default user: a misrouted request would otherwise see someone's history.
func TestTrustedHeaderRequiresTheUserHeader(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	if _, err := (TrustedHeader{}).Identify(r); err != ErrNoIdentity {
		t.Fatalf("err = %v, want ErrNoIdentity", err)
	}
	r.Header.Set("X-Remote-User", "alice")
	id, err := TrustedHeader{UserHeader: "X-Remote-User"}.Identify(r)
	if err != nil || id.Owner() != "alice" {
		t.Fatalf("custom header: %+v, %v", id, err)
	}
}

func TestTrustedHeaderCanRequireATrustedPeer(t *testing.T) {
	t.Parallel()
	nets, err := ParseCIDRs("127.0.0.1, 10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	p := TrustedHeader{TrustedProxies: nets}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-User", "alice")
	r.RemoteAddr = "192.168.1.5:4000"
	if _, err := p.Identify(r); err != ErrUntrustedPeer {
		t.Fatalf("untrusted peer: err = %v", err)
	}
	r.RemoteAddr = "10.2.3.4:4000"
	if _, err := p.Identify(r); err != nil {
		t.Fatalf("trusted peer: %v", err)
	}
	if _, err := ParseCIDRs("not-an-address"); err == nil {
		t.Fatal("ParseCIDRs accepted garbage")
	}
}

func TestMiddlewareAttachesIdentityOrRefuses(t *testing.T) {
	t.Parallel()
	var seen Identity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	refused := func(w http.ResponseWriter, msg string) { http.Error(w, msg, http.StatusUnauthorized) }
	h := Middleware(TrustedHeader{}, refused, next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no header: %d", rr.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-User", "alice")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK || seen.Owner() != "alice" {
		t.Fatalf("with header: %d, %+v", rr.Code, seen)
	}
}
