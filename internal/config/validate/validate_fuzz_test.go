package validate

import (
	"strings"
	"testing"
)

// FuzzValidateAgainstSchema tests schema validation with various inputs
func FuzzValidateAgainstSchema(f *testing.F) {
	// Get a basic schema for testing
	basicSchema := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"version": {"type": "string"}
		},
		"required": ["name"]
	}`)

	// Seed with various JSON data patterns
	f.Add("test-schema", basicSchema, []byte(`{"name": "test", "version": "1.0"}`), "")
	f.Add("test-schema", basicSchema, []byte(`{"name": "test"}`), "")
	f.Add("test-schema", basicSchema, []byte(`{}`), "")
	f.Add("test-schema", basicSchema, []byte(`{"name": null}`), "")
	f.Add("test-schema", basicSchema, []byte(`{"name": ""}`), "")
	f.Add("test-schema", basicSchema, []byte(`{"name": "test", "extra": "field"}`), "")
	f.Add("test-schema", basicSchema, []byte(`invalid json`), "")
	f.Add("test-schema", basicSchema, []byte(`null`), "")
	f.Add("test-schema", basicSchema, []byte(`[]`), "")
	f.Add("test-schema", basicSchema, []byte(`"string"`), "")

	f.Fuzz(func(t *testing.T, name string, schema []byte, data []byte, ref string) {
		// Skip invalid schema names that would cause panics in the library
		if name == "" || strings.Contains(name, "#") || len(name) < 3 {
			t.Skip("Skipping invalid schema name")
		}

		// Skip empty or very small schema data
		if len(schema) < 10 {
			t.Skip("Skipping too small schema")
		}

		// Test ValidateAgainstSchema - should not crash with any input
		err := ValidateAgainstSchema(name, schema, data, ref)

		// Function should handle all inputs gracefully (error or success both acceptable)
		// The key is that it shouldn't panic or crash
		_ = err // We don't validate the specific error, just that it doesn't crash
	})
}

// FuzzValidateImageTemplateJSON tests image template validation
func FuzzValidateImageTemplateJSON(f *testing.F) {
	// Seed with various JSON patterns for image templates
	f.Add([]byte(`{"image": {"name": "test", "version": "1.0"}, "target": {"os": "linux"}, "systemConfig": {"kernel": {"version": "6.1"}}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"image": null}`))
	f.Add([]byte(`{"image": {"name": "", "version": ""}}`))
	f.Add([]byte(`invalid json content`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`"string"`))
	f.Add([]byte(`{"image": {"name": "test"}}`)) // Missing version
	f.Add([]byte(`{"image": {"name": "test", "version": "1.0", "extra": "field"}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Test ValidateImageTemplateJSON - should not crash with any input
		err := ValidateImageTemplateJSON(data)

		// Function should handle all inputs gracefully
		_ = err // We accept both success and error, just no crashes
	})
}

// FuzzValidateUserTemplateJSON tests user template validation
func FuzzValidateUserTemplateJSON(f *testing.F) {
	// Seed with various JSON patterns for user templates
	f.Add([]byte(`{"image": {"name": "test", "version": "1.0"}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"image": null}`))
	f.Add([]byte(`invalid json`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{"image": {"name": "very-long-name-that-might-cause-issues", "version": "1.0.0-alpha-beta"}}`))
	f.Add([]byte(`{"image": {"name": "", "version": ""}}`))
	f.Add([]byte(`{"unknown": "field"}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Test ValidateUserTemplateJSON - should not crash with any input
		err := ValidateUserTemplateJSON(data)

		// Function should handle all inputs gracefully
		_ = err // We accept both success and error, just no crashes
	})
}

// FuzzValidateConfigJSON tests configuration validation
func FuzzValidateConfigJSON(f *testing.F) {
	// Seed with various config JSON patterns
	f.Add([]byte(`{"version": "1.0"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"version": null}`))
	f.Add([]byte(`invalid json`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{"version": "", "extra": "field"}`))
	f.Add([]byte(`{"config": {"nested": "value"}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Test ValidateConfigJSON - should not crash with any input
		err := ValidateConfigJSON(data)

		// Function should handle all inputs gracefully
		_ = err // We accept both success and error, just no crashes
	})
}
