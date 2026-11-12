// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A local server reports itself as such: one build at a time, no executor
// phases (the UI keeps its log-derived ones), and the version stamps.
func TestGetServerInfoDescribesTheLocalServer(t *testing.T) {
	s, _ := newTestServer(t)
	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/server", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var info struct {
		Version  string `json:"version"`
		Executor struct {
			Name          string `json:"name"`
			MaxConcurrent int    `json:"maxConcurrent"`
			Phases        *[]any `json:"phases"`
		} `json:"executor"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Version == "" || info.Executor.Name != "local" || info.Executor.MaxConcurrent != 1 || info.Executor.Phases != nil {
		t.Errorf("server info = %+v", info)
	}
}

// The history list carries the server's capacity, so the UI can gate Compose
// without provoking a 409.
func TestListBuildsReportsMaxConcurrent(t *testing.T) {
	s, _ := newTestServer(t)
	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var list struct {
		MaxConcurrent int `json:"maxConcurrent"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.MaxConcurrent != 1 {
		t.Errorf("maxConcurrent = %d, want 1", list.MaxConcurrent)
	}
}
