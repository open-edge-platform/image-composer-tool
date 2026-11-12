// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/api/auth"
)

// newHeaderAuthServer is newTestServer behind a trusted-header identity.
func newHeaderAuthServer(t *testing.T) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	s.auth = auth.TrustedHeader{}
	return s
}

func as(user string, req *http.Request) *http.Request {
	req.Header.Set("X-Forwarded-User", user)
	return req
}

// With no identity provider configured the API behaves exactly as before: one
// implicit local user who sees everything.
func TestDefaultIdentityIsOneLocalUser(t *testing.T) {
	s, _ := newTestServer(t)
	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var me map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &me)
	if me["owner"] != "local" {
		t.Errorf("me = %v", me)
	}
	rr = s.do(httptest.NewRequest(http.MethodGet, "/api/v1/server", nil))
	var info struct {
		Executor struct {
			Name          string `json:"name"`
			MaxConcurrent int    `json:"maxConcurrent"`
		}
		Auth struct {
			Mode string `json:"mode"`
		}
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &info)
	if info.Executor.Name != "local" || info.Executor.MaxConcurrent != 1 || info.Auth.Mode != "none" {
		t.Errorf("server info = %+v", info)
	}
}

// Behind a login proxy every API call needs the identity header; the SPA does not.
func TestTrustedHeaderGuardsOnlyTheAPI(t *testing.T) {
	s := newHeaderAuthServer(t)
	for _, p := range []string{"/api/v1/me", "/api/v1/builds", "/api/v1/manifest"} {
		if rr := s.do(httptest.NewRequest(http.MethodGet, p, nil)); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s without identity = %d, want 401", p, rr.Code)
		}
	}
	rr := s.do(as("alice", httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)))
	if rr.Code != http.StatusOK {
		t.Fatalf("/me with identity = %d: %s", rr.Code, rr.Body)
	}
	var me map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &me)
	if me["owner"] != "alice" {
		t.Errorf("me = %v", me)
	}
	// The SPA is served without one (there is no embedded build in tests, so a
	// 404 from the mux is the expected "not refused" answer).
	if rr := s.do(httptest.NewRequest(http.MethodGet, "/", nil)); rr.Code == http.StatusUnauthorized {
		t.Errorf("SPA root refused: %d", rr.Code)
	}
}

// Each user sees their own builds; another user's build does not exist for them.
func TestBuildsAreOwnedByTheCaller(t *testing.T) {
	s := newHeaderAuthServer(t)
	body := `{"compose":{"vertical":"robotics","sku":"amr","platform":"wcl","os":"ubuntu24","imageType":"iso"}}`
	rr := s.do(as("alice", httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(body))))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("start = %d: %s", rr.Code, rr.Body)
	}
	var acc struct {
		BuildID string `json:"buildId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &acc)
	h, _ := s.svc.Build(acc.BuildID)
	<-h.Done()

	list := func(user string) int {
		rr := s.do(as(user, httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil)))
		var bl struct {
			Builds []any `json:"builds"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &bl)
		return len(bl.Builds)
	}
	if list("alice") != 1 || list("bob") != 0 {
		t.Fatalf("alice sees %d, bob sees %d", list("alice"), list("bob"))
	}
	for _, p := range []string{"/details", "/artifacts", "/logs", "/template", "/logfile"} {
		rr := s.do(as("bob", httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+acc.BuildID+p, nil)))
		if rr.Code != http.StatusNotFound {
			t.Errorf("bob GET %s = %d, want 404", p, rr.Code)
		}
	}
	rr = s.do(as("bob", httptest.NewRequest(http.MethodPost, "/api/v1/builds/"+acc.BuildID+"/retry", nil)))
	if rr.Code != http.StatusNotFound {
		t.Errorf("bob retry = %d, want 404", rr.Code)
	}
	rr = s.do(as("alice", httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+acc.BuildID+"/details", nil)))
	if rr.Code != http.StatusOK {
		t.Errorf("alice details = %d", rr.Code)
	}
	// The record on disk names the owner, so history survives a restart per user.
	details, _ := s.svc.BuildDetails(acc.BuildID)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(details.WorkDir), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Owner string `json:"owner"`
	}
	_ = json.Unmarshal(data, &m)
	if m.Owner != "alice" {
		t.Errorf("persisted owner = %q", m.Owner)
	}
}
