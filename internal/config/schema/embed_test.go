package schema

import (
	"encoding/json"
	"testing"
)

func TestEmbeddedSchemasNonEmpty(t *testing.T) {
	tests := []struct {
		name   string
		schema []byte
	}{
		{"ImageTemplateSchema", ImageTemplateSchema},
		{"ConfigSchema", ConfigSchema},
		{"ChrootenvSchema", ChrootenvSchema},
		{"OsConfigSchema", OsConfigSchema},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.schema) == 0 {
				t.Fatalf("%s: expected non-empty bytes, got empty", tt.name)
			}
		})
	}
}

func TestEmbeddedSchemasValidJSON(t *testing.T) {
	tests := []struct {
		name   string
		schema []byte
	}{
		{"ImageTemplateSchema", ImageTemplateSchema},
		{"ConfigSchema", ConfigSchema},
		{"ChrootenvSchema", ChrootenvSchema},
		{"OsConfigSchema", OsConfigSchema},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(tt.schema, &doc); err != nil {
				t.Fatalf("%s: invalid JSON: %v", tt.name, err)
			}
		})
	}
}

func TestEmbeddedSchemasHaveMetadataFields(t *testing.T) {
	tests := []struct {
		name       string
		schema     []byte
		wantSchema string
		wantID     string
	}{
		{
			name:       "ImageTemplateSchema",
			schema:     ImageTemplateSchema,
			wantSchema: "https://json-schema.org/draft/2020-12/schema",
			wantID:     "os-image-template.schema.json",
		},
		{
			name:       "ConfigSchema",
			schema:     ConfigSchema,
			wantSchema: "https://json-schema.org/draft/2020-12/schema",
			wantID:     "https://github.com/open-edge-platform/image-composer-tool/schemas/image-composer-tool-config.schema.json",
		},
		{
			name:       "ChrootenvSchema",
			schema:     ChrootenvSchema,
			wantSchema: "https://json-schema.org/draft/2020-12/schema",
			wantID:     "chrootenv-config.schema.json",
		},
		{
			name:       "OsConfigSchema",
			schema:     OsConfigSchema,
			wantSchema: "https://json-schema.org/draft/2020-12/schema",
			wantID:     "os-config.schema.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(tt.schema, &doc); err != nil {
				t.Fatalf("unexpected JSON parse error: %v", err)
			}

			gotSchema, _ := doc["$schema"].(string)
			if gotSchema != tt.wantSchema {
				t.Errorf("$schema: got %q, want %q", gotSchema, tt.wantSchema)
			}

			gotID, _ := doc["$id"].(string)
			if gotID != tt.wantID {
				t.Errorf("$id: got %q, want %q", gotID, tt.wantID)
			}
		})
	}
}
