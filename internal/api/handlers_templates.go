package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/template"
)

// templateListResponse matches the OpenAPI response for GET /api/v1/templates.
type templateListResponse struct {
	Templates []templateInfoJSON `json:"templates"`
	Count     int                `json:"count"`
}

// templateDetailResponse matches the OpenAPI TemplateDetail schema.
// Extends TemplateInfo with the raw YAML content.
type templateDetailResponse struct {
	templateInfoJSON
	YAML string `json:"yaml"`
}

// handleListTemplates returns a summary list of all templates.
// GET /api/v1/templates
//
// Calls template.ScanTemplates() from the shared library to scan
// the image-templates/ directory.
func handleListTemplates(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templates, err := template.ScanTemplates(s.config.TemplatesDir)
		if err != nil {
			respondError(w, http.StatusInternalServerError, ErrCodeEngineUnavailable,
				"Failed to scan templates: "+err.Error(), nil)
			return
		}

		// Convert internal TemplateInfo structs to the OpenAPI JSON shape.
		jsonTemplates := make([]templateInfoJSON, 0, len(templates))
		for _, t := range templates {
			jsonTemplates = append(jsonTemplates, convertTemplateInfo(t))
		}

		resp := templateListResponse{
			Templates: jsonTemplates,
			Count:     len(jsonTemplates),
		}

		respondJSON(w, http.StatusOK, resp)
	}
}

// handleGetTemplate returns the full details of a specific template.
// GET /api/v1/templates/{name}
//
// Calls template.ParseTemplate() from the shared library to parse
// the template file and return metadata + raw YAML.
func handleGetTemplate(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			respondError(w, http.StatusBadRequest, ErrCodeTemplateNotFound,
				"Template name is required", nil)
			return
		}

		// Reject any name that could escape the templates directory.
		// {name} must be a bare file name — no path separators, no "..".
		// This guards against traversal like "..%2fsecret" (URL-decoded to
		// "../secret") which filepath.Join would otherwise resolve outside
		// TemplatesDir.
		if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
			respondError(w, http.StatusBadRequest, ErrCodeTemplateNotFound,
				"Invalid template name", nil)
			return
		}

		// Ensure the name has a .yml extension for file lookup.
		if !strings.HasSuffix(name, ".yml") {
			name = name + ".yml"
		}

		filePath := filepath.Join(s.config.TemplatesDir, name)

		// Defense in depth: verify the cleaned path is still inside the
		// templates directory before touching the filesystem.
		if !pathWithinDir(s.config.TemplatesDir, filePath) {
			respondError(w, http.StatusBadRequest, ErrCodeTemplateNotFound,
				"Invalid template name", nil)
			return
		}

		t, err := template.ParseTemplate(filePath)
		if err != nil {
			respondError(w, http.StatusNotFound, ErrCodeTemplateNotFound,
				"Template '"+strings.TrimSuffix(name, ".yml")+"' not found", nil)
			return
		}

		resp := templateDetailResponse{
			templateInfoJSON: convertTemplateInfo(t),
			YAML:             string(t.RawContent),
		}

		respondJSON(w, http.StatusOK, resp)
	}
}

// convertTemplateInfo converts an internal TemplateInfo to the OpenAPI JSON shape.
func convertTemplateInfo(t *template.TemplateInfo) templateInfoJSON {
	info := templateInfoJSON{
		FileName:     t.FileName,
		ImageName:    t.ImageName,
		ImageVersion: t.ImageVersion,
		Distribution: t.Distribution,
		Architecture: t.Architecture,
		OS:           t.OS,
		ImageType:    t.ImageType,
		Packages:     t.Packages,
		Metadata: templateMetaJSON{
			Description:    t.Metadata.Description,
			UseCases:       t.Metadata.UseCases,
			Keywords:       t.Metadata.Keywords,
			Capabilities:   t.Metadata.Capabilities,
			RecommendedFor: t.Metadata.RecommendedFor,
		},
	}

	// Ensure non-nil slices in JSON output ([] instead of null).
	if info.Packages == nil {
		info.Packages = []string{}
	}
	if info.Metadata.UseCases == nil {
		info.Metadata.UseCases = []string{}
	}
	if info.Metadata.Keywords == nil {
		info.Metadata.Keywords = []string{}
	}
	if info.Metadata.Capabilities == nil {
		info.Metadata.Capabilities = []string{}
	}
	if info.Metadata.RecommendedFor == nil {
		info.Metadata.RecommendedFor = []string{}
	}

	return info
}
