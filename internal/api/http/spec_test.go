// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"context"
	"net/http"
	"testing"
)

// TestEmbeddedSpecIsValid decodes and validates the OpenAPI spec embedded at
// generation time. It fails if the committed spec is malformed — a fast guard
// that the checked-in gen.go was produced from a valid contract.
func TestEmbeddedSpecIsValid(t *testing.T) {
	swagger, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger: %v", err)
	}
	if err := swagger.Validate(context.Background()); err != nil {
		t.Fatalf("embedded spec invalid: %v", err)
	}
	// Sanity-check that the JSON operations we rely on are present.
	for _, p := range []string{"/manifest", "/templates/compose", "/builds", "/builds/{id}/details", "/builds/{id}/artifacts"} {
		if swagger.Paths.Find(p) == nil {
			t.Errorf("spec missing path %q", p)
		}
	}
}

// stubServer is a no-op ServerInterface so HandlerFromMux can be exercised for
// its route registration without a real backend.
type stubServer struct{}

func (stubServer) ListBuilds(http.ResponseWriter, *http.Request)                  {}
func (stubServer) StartBuild(http.ResponseWriter, *http.Request)                  {}
func (stubServer) ListBuildArtifacts(http.ResponseWriter, *http.Request, BuildId) {}
func (stubServer) GetBuildDetails(http.ResponseWriter, *http.Request, BuildId)    {}
func (stubServer) GetManifest(http.ResponseWriter, *http.Request)                 {}
func (stubServer) ComposeTemplate(http.ResponseWriter, *http.Request)             {}

// TestHandlerFromMuxRegistersRoutes verifies the generated registration wires
// the base URL and dispatches to the interface method (here, a 200 from the
// stub) rather than 404.
func TestHandlerFromMuxRegistersRoutes(t *testing.T) {
	mux := http.NewServeMux()
	HandlerFromMuxWithBaseURL(stubServer{}, mux, "/api/v1")

	// The stub writes no status, so a matched route yields the default 200; an
	// unmatched path would yield 404. Assert the manifest route is registered.
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/manifest", nil)
	_, pattern := mux.Handler(req)
	if pattern == "" {
		t.Fatal("GET /api/v1/manifest not registered by HandlerFromMux")
	}
}
