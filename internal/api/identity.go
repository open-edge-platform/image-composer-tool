// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/api/auth"
	httpapi "github.com/open-edge-platform/image-composer-tool/internal/api/http"
)

// ownerOf returns the key the authenticated caller's builds are filed under.
//
// A request that never passed through the auth middleware has no identity and
// gets "", which every ownership check treats as owning only ownerless builds.
// That is the safe direction: a route accidentally mounted outside the
// middleware serves nobody's history rather than everybody's.
func ownerOf(r *http.Request) string {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		return ""
	}
	return id.Owner()
}

// authenticateAPI identifies the caller of every /api/ request and leaves
// everything else public. The SPA and its assets have to stay reachable
// without an identity: with a login proxy in front, the proxy decides who may
// load them at all, and without one there is nothing to identify against.
func authenticateAPI(p auth.Provider, next http.Handler) http.Handler {
	protected := auth.Middleware(p, func(w http.ResponseWriter, msg string) {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", msg)
	}, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			protected.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireOwner writes the standard not-found response unless the caller may see
// the build, and reports whether the request may proceed.
//
// The routes that stream logs or serve files are registered on the mux directly
// rather than generated from the OpenAPI spec, so they do not get an ownership
// check for free; each one calls this first. Not-found rather than forbidden,
// so that nobody can learn that another user's build exists.
func (s *Server) requireOwner(w http.ResponseWriter, r *http.Request, id string) bool {
	if s.svc.OwnsBuild(id, ownerOf(r)) {
		return true
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
	return false
}

// GetMe handles GET /me.
func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "no identity on the request")
		return
	}
	me := httpapi.Me{Owner: id.Owner(), DisplayName: id.DisplayName(), Email: optStr(id.Email)}
	if len(id.Groups) > 0 {
		groups := append([]string(nil), id.Groups...)
		me.Groups = &groups
	}
	writeJSON(w, http.StatusOK, me)
}
