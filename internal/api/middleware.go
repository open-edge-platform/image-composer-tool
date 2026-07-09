// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// withMiddleware wraps the mux with CORS, request logging, and panic recovery.
func withMiddleware(next http.Handler) http.Handler {
	return recoverPanic(logRequests(cors(next)))
}

// cors permits cross-origin calls only from localhost origins (e.g. the Vite dev
// server). This API can trigger privileged builds, so a wildcard origin would let
// any web page issue requests against a locally running server; instead we echo
// the request Origin only when it is a loopback host.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if isLocalhostOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLocalhostOrigin reports whether an Origin header refers to a loopback host
// (localhost / 127.0.0.1 / [::1]), ignoring scheme and port.
func isLocalhostOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Logger().Infof("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Logger().Errorf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, http.StatusInternalServerError, "INTERNAL", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// writeJSON serializes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Logger().Errorf("encoding response: %v", err)
	}
}

// errorBody mirrors the Error schema in the OpenAPI spec.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var b errorBody
	b.Error.Code = code
	b.Error.Message = msg
	writeJSON(w, status, b)
}
