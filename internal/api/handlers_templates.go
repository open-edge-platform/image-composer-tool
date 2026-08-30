package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/template"
	"github.com/open-edge-platform/image-composer-tool/internal/config/validate"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
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

type templateValidateRequest struct {
	YAML string `json:"yaml"`
}

type validationResult struct {
	Valid  bool              `json:"valid"`
	Errors []validationError `json:"errors"`
}

type validationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
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

func handleValidate(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req templateValidateRequest
		if !decodeJSON(w, r, &req) {
			return
		}

		if req.YAML == "" {
			respondJSON(w, http.StatusOK, validationResult{
				Valid:  false,
				Errors: []validationError{{Path: "/", Message: "YAML content is required"}},
			})
			return
		}

		var yamlDoc interface{}
		if err := yaml.Unmarshal([]byte(req.YAML), &yamlDoc); err != nil {
			respondJSON(w, http.StatusOK, validationResult{
				Valid:  false,
				Errors: []validationError{{Path: "/", Message: "Invalid YAML syntax: " + err.Error()}},
			})
			return
		}

		jsonBytes, err := json.Marshal(convertYAMLToJSONCompatible(yamlDoc))
		if err != nil {
			respondJSON(w, http.StatusOK, validationResult{
				Valid:  false,
				Errors: []validationError{{Path: "/", Message: "Failed to convert YAML to JSON: " + err.Error()}},
			})
			return
		}

		if err := validate.ValidateUserTemplateJSON(jsonBytes); err != nil {
			errors := extractValidationErrors(err)
			respondJSON(w, http.StatusOK, validationResult{
				Valid:  false,
				Errors: errors,
			})
			return
		}

		respondJSON(w, http.StatusOK, validationResult{
			Valid:  true,
			Errors: []validationError{},
		})
	}
}

func extractValidationErrors(err error) []validationError {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []validationError{{Path: "/", Message: err.Error()}}
	}

	var result []validationError
	collectErrors(ve, &result)

	if len(result) == 0 {
		result = append(result, validationError{
			Path:    ve.InstanceLocation,
			Message: ve.Message,
		})
	}
	return result
}

func collectErrors(ve *jsonschema.ValidationError, result *[]validationError) {
	if len(ve.Causes) == 0 {
		path := ve.InstanceLocation
		if path == "" {
			path = "/"
		}
		*result = append(*result, validationError{
			Path:    path,
			Message: ve.Message,
		})
		return
	}
	for _, cause := range ve.Causes {
		collectErrors(cause, result)
	}
}

func convertYAMLToJSONCompatible(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, v := range val {
			result[k] = convertYAMLToJSONCompatible(v)
		}
		return result
	case map[interface{}]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, v := range val {
			result[fmt.Sprintf("%v", k)] = convertYAMLToJSONCompatible(v)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v := range val {
			result[i] = convertYAMLToJSONCompatible(v)
		}
		return result
	default:
		return v
	}
}
