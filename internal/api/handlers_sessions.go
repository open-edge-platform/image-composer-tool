package api

import (
	"net/http"
)

// handleCreateSession creates a new conversation session.
// POST /api/v1/sessions
//
// Returns 201 with the full Session JSON on success.
// Returns 503 if the maximum number of sessions has been reached.
func handleCreateSession(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := s.sessionMgr.Create()
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, ErrCodeRateLimited,
				err.Error(), nil)
			return
		}

		respondJSON(w, http.StatusCreated, session)
	}
}

// handleGetSession retrieves an existing session by ID.
// GET /api/v1/sessions/{id}
//
// Returns 200 with the full Session JSON on success.
// Returns 404 with SESSION_NOT_FOUND if the session does not exist.
// Returns 410 with SESSION_EXPIRED if the session has timed out.
func handleGetSession(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			respondError(w, http.StatusBadRequest, ErrCodeSessionNotFound,
				"Session ID is required", nil)
			return
		}

		session, err := s.sessionMgr.Get(id)
		if err != nil {
			if sessErr, ok := err.(*SessionError); ok {
				switch sessErr.Code {
				case ErrCodeSessionExpired:
					// 410 Gone — the session existed but has timed out.
					// Per OpenAPI spec, clean up the expired session.
					_ = s.sessionMgr.Delete(id)
					respondError(w, http.StatusGone, ErrCodeSessionExpired,
						sessErr.Error(), nil)
					return
				default:
					respondError(w, http.StatusNotFound, ErrCodeSessionNotFound,
						sessErr.Error(), nil)
					return
				}
			}
			respondError(w, http.StatusInternalServerError, ErrCodeEngineUnavailable,
				"Failed to retrieve session: "+err.Error(), nil)
			return
		}

		// Touch the session to keep it alive.
		s.sessionMgr.Touch(id)

		respondJSON(w, http.StatusOK, session)
	}
}

// handleDeleteSession removes a session by ID.
// DELETE /api/v1/sessions/{id}
//
// Returns 204 No Content on success.
// Returns 404 with SESSION_NOT_FOUND if the session does not exist.
func handleDeleteSession(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			respondError(w, http.StatusBadRequest, ErrCodeSessionNotFound,
				"Session ID is required", nil)
			return
		}

		err := s.sessionMgr.Delete(id)
		if err != nil {
			if sessErr, ok := err.(*SessionError); ok {
				respondError(w, http.StatusNotFound, ErrCodeSessionNotFound,
					sessErr.Error(), nil)
				return
			}
			respondError(w, http.StatusInternalServerError, ErrCodeEngineUnavailable,
				"Failed to delete session: "+err.Error(), nil)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
