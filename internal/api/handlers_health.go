package api

import (
	"net/http"
	"time"
)

// healthResponse matches the OpenAPI HealthResponse schema.
type healthResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// engineStatsResponse matches the OpenAPI EngineStats schema.
type engineStatsResponse struct {
	Initialized    bool            `json:"initialized"`
	IndexedAt      time.Time       `json:"indexed_at"`
	TemplateCount  int             `json:"template_count"`
	Provider       string          `json:"provider"`
	EmbeddingModel string          `json:"embedding_model"`
	CacheEnabled   bool            `json:"cache_enabled"`
	CacheStats     *cacheStatsJSON `json:"cache_stats"`
}

// cacheStatsJSON matches the OpenAPI CacheStats schema.
type cacheStatsJSON struct {
	EntryCount int       `json:"entry_count"`
	TotalSize  int64     `json:"total_size"`
	ModelID    string    `json:"model_id"`
	Dimensions int       `json:"dimensions"`
	CreatedAt  time.Time `json:"created_at"`
}

// handleHealth returns the service and engine readiness status.
// GET /api/v1/health
//
// Returns "ok" when the engine is initialized and ready, "initializing"
// when indexing is not yet complete. HTTP 200 in both cases — inspect
// the "status" field for engine readiness.
func handleHealth(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := s.engine.GetStats()

		resp := healthResponse{}
		if stats.Initialized {
			resp.Status = "ok"
		} else {
			resp.Status = "initializing"
		}

		respondJSON(w, http.StatusOK, resp)
	}
}

// handleStats returns detailed engine statistics.
// GET /api/v1/engine/stats
//
// Returns template count, provider info, and embedding cache metrics.
func handleStats(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := s.engine.GetStats()

		resp := engineStatsResponse{
			Initialized:    stats.Initialized,
			IndexedAt:      stats.IndexedAt,
			TemplateCount:  stats.TemplateCount,
			Provider:       stats.Provider,
			EmbeddingModel: stats.EmbeddingModel,
			CacheEnabled:   stats.CacheEnabled,
		}

		if stats.CacheStats != nil {
			resp.CacheStats = &cacheStatsJSON{
				EntryCount: stats.CacheStats.EntryCount,
				TotalSize:  stats.CacheStats.TotalSize,
				ModelID:    stats.CacheStats.ModelID,
				Dimensions: stats.CacheStats.Dimensions,
				CreatedAt:  stats.CacheStats.CreatedAt,
			}
		}

		respondJSON(w, http.StatusOK, resp)
	}
}
