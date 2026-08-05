package validate

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config/schema"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Issue is a single validation problem tied to a field path. It is the
// structured counterpart to the errors returned by ValidateUserTemplateJSON /
// ValidateImageTemplateJSON: instead of one formatted string, callers that need
// to point at individual fields (the web UI's inline validation) get one Issue
// per problem, each carrying the JSON path of the offending value.
type Issue struct {
	// Path is the dotted field path of the offending value (e.g. "target.os",
	// "disk.partitions.0.type"). Empty when the problem is document-wide (for
	// example a value that isn't a valid template object at all).
	Path string
	// Message describes the problem in human-readable form.
	Message string
	// Severity is "error" or "warning". All issues are errors today; the field
	// exists so warnings can be added without a signature change.
	Severity string
}

// SeverityError and SeverityWarning are the valid Issue.Severity values.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// ValidateUserTemplateIssues validates a user (minimal) template and returns a
// structured list of issues instead of a single wrapped error. A nil/empty
// result means the template is valid.
//
// It runs the same checks as ValidateUserTemplateJSON — the same schema
// subschema plus the same post-schema semantic checks (auto-expand, FDE) — so
// the pass/fail verdict here matches what a build of the same template would
// reach. It differs deliberately in one way: where ValidateUserTemplateJSON
// stops at the first failure, this keeps going and collects every problem, since
// the point is to show the user all their field errors at once rather than one
// per round-trip. Kept alongside (not replacing) ValidateUserTemplateJSON so CLI
// callers that only need pass/fail are unaffected.
func ValidateUserTemplateIssues(data []byte) []Issue {
	var issues []Issue

	if err := ValidateAgainstSchema(imageSchemaName, schema.ImageTemplateSchema, data, userRef); err != nil {
		issues = append(issues, schemaErrorIssues(err)...)
	}

	// The semantic checks below unmarshal into map[string]interface{}, so a
	// document that isn't a JSON object makes both fail on the unmarshal itself.
	// The schema check above has already reported that ("expected object, but got
	// array"); running them anyway would append two more issues pointing at
	// unrelated fields (disk, systemConfig.fde.passphraseFile) that the input
	// never mentioned. Bail out instead — the caller has the real problem.
	if !isJSONObject(data) {
		return issues
	}

	// Post-schema semantic checks. These return a single error each (not a schema
	// tree), so map each to one issue anchored at the field it concerns.
	if err := validateAutoExpandLastPartitionConstraints(data, false); err != nil {
		issues = append(issues, Issue{Path: "disk", Message: err.Error(), Severity: SeverityError})
	}
	if err := validateFDEConstraints(data); err != nil {
		issues = append(issues, Issue{Path: "systemConfig.fde.passphraseFile", Message: err.Error(), Severity: SeverityError})
	}

	return issues
}

// isJSONObject reports whether data is a JSON object, the only shape the
// semantic checks can inspect. The non-nil guard matters: json.Unmarshal of the
// literal `null` succeeds into a nil map, so without it a `null` document would
// be treated as an object and fall into the semantic checks.
func isJSONObject(data []byte) bool {
	var doc map[string]interface{}
	return json.Unmarshal(data, &doc) == nil && doc != nil
}

// schemaErrorIssues turns the error from ValidateAgainstSchema into field-level
// issues. On a schema validation failure it unwraps the underlying
// *jsonschema.ValidationError and walks its Causes, emitting one issue per leaf
// (the most specific problems) using InstanceLocation as the path. Any other
// error (malformed JSON, or an internal schema-load failure) has no field path,
// so it becomes a single pathless issue.
func schemaErrorIssues(err error) []Issue {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []Issue{{Message: err.Error(), Severity: SeverityError}}
	}

	leaves := collectLeafErrors(ve, nil)
	if len(leaves) == 0 {
		// Defensive: a ValidationError with no nested causes. Use the root itself.
		return []Issue{{Path: pointerToPath(ve.InstanceLocation), Message: ve.Message, Severity: SeverityError}}
	}

	issues := make([]Issue, 0, len(leaves))
	seen := make(map[string]bool, len(leaves))
	for _, leaf := range leaves {
		iss := Issue{Path: pointerToPath(leaf.InstanceLocation), Message: leaf.Message, Severity: SeverityError}
		// The conditional (allOf/if-then/anyOf) branches in the schema can surface
		// the same (path, message) more than once; report each once.
		key := iss.Path + "\x00" + iss.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		issues = append(issues, iss)
	}
	return issues
}

// collectLeafErrors returns the leaf errors of a jsonschema validation tree —
// the nodes with no further causes, which carry the most specific field path and
// message. Intermediate nodes only say "some sub-schema failed", so the leaves
// are what a user can act on.
func collectLeafErrors(ve *jsonschema.ValidationError, acc []*jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(ve.Causes) == 0 {
		return append(acc, ve)
	}
	for _, c := range ve.Causes {
		acc = collectLeafErrors(c, acc)
	}
	return acc
}

// pointerToPath converts a JSON Pointer (jsonschema's InstanceLocation, e.g.
// "/target/os" or "/disk/partitions/0/type") into a dotted field path
// ("target.os", "disk.partitions.0.type") for display. The empty pointer (the
// whole document) maps to "". Handles the JSON Pointer escapes ~1 (/) and ~0 (~).
func pointerToPath(ptr string) string {
	if ptr == "" || ptr == "/" {
		return ""
	}
	tokens := strings.Split(strings.TrimPrefix(ptr, "/"), "/")
	for i, t := range tokens {
		t = strings.ReplaceAll(t, "~1", "/")
		t = strings.ReplaceAll(t, "~0", "~")
		tokens[i] = t
	}
	return strings.Join(tokens, ".")
}
