package api

import (
	"net/http"
	"strings"
)

// CORSConfig holds CORS middleware configuration.
// Default values match the ADR configuration section.
type CORSConfig struct {
	// AllowedOrigins is the list of origins permitted to make cross-origin
	// requests. Default: ["http://localhost:3000"] (React dev server).
	AllowedOrigins []string

	// AllowedMethods is the list of HTTP methods permitted in cross-origin
	// requests. Default: ["GET", "POST", "PUT", "DELETE", "OPTIONS"].
	// PUT is included from day one for future template update endpoints.
	AllowedMethods []string

	// AllowedHeaders is the list of HTTP headers the browser is allowed to
	// send. Default: ["Content-Type", "Authorization"].
	AllowedHeaders []string
}

// DefaultCORSConfig returns the default CORS configuration as specified
// in the ADR configuration section, with PUT and Content-Type included
// from day one for future-proofing.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}
}

// withCORS wraps an http.Handler with CORS middleware.
// It handles preflight OPTIONS requests and adds the appropriate
// Access-Control-* headers to all responses.
func withCORS(handler http.Handler, config CORSConfig) http.Handler {
	methods := strings.Join(config.AllowedMethods, ", ")
	headers := strings.Join(config.AllowedHeaders, ", ")

	// Build a set of allowed origins for fast lookup.
	allowedOriginSet := make(map[string]bool, len(config.AllowedOrigins))
	for _, origin := range config.AllowedOrigins {
		allowedOriginSet[origin] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// The response depends on the request Origin, so caches must key on
		// it. Always advertise this, even when the origin is not allowed.
		w.Header().Add("Vary", "Origin")

		// Check if the request origin is allowed.
		if origin != "" && allowedOriginSet[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)

			// Access-Control-Allow-Methods/Headers are only meaningful on the
			// preflight (OPTIONS) response; they are ignored on actual
			// responses, so only set them there.
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
			}
		}

		// Handle preflight requests: the browser sends an OPTIONS request
		// before the actual request to check if the server allows it.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		handler.ServeHTTP(w, r)
	})
}
