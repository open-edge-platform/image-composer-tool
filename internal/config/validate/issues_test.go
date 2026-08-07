package validate

import (
	"encoding/json"
	"testing"

	"sigs.k8s.io/yaml"
)

// toJSON converts a YAML template body to the JSON bytes the validators consume,
// mirroring the build path (yaml -> interface{} -> json).
func toJSON(t *testing.T, y string) []byte {
	t.Helper()
	var raw interface{}
	if err := yaml.Unmarshal([]byte(y), &raw); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	return data
}

// pathsOf collects the Path of every issue for order-independent assertions.
func pathsOf(issues []Issue) map[string]string {
	m := make(map[string]string, len(issues))
	for _, iss := range issues {
		m[iss.Path] = iss.Message
	}
	return m
}

func TestValidateUserTemplateIssues_Valid(t *testing.T) {
	// Minimal valid user template: image{name,version} + target{os,dist,arch,imageType}.
	y := `image:
  name: my-image
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
`
	issues := ValidateUserTemplateIssues(toJSON(t, y))
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d: %+v", len(issues), issues)
	}
}

func TestValidateUserTemplateIssues_MultipleFieldErrors(t *testing.T) {
	// Four distinct problems: bad image.name pattern, os not in enum, imageType
	// not in enum, and target missing dist+arch.
	y := `image:
  name: "-bad-name"
  version: "1.0"
target:
  os: not-an-os
  imageType: qcow2
`
	issues := ValidateUserTemplateIssues(toJSON(t, y))
	if len(issues) == 0 {
		t.Fatal("expected issues, got none")
	}
	got := pathsOf(issues)
	for _, want := range []string{"image.name", "target.os", "target.imageType", "target"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing issue for path %q; got paths %v", want, keys(got))
		}
	}
	// Every issue is an error severity with a non-empty, human-readable message.
	for _, iss := range issues {
		if iss.Severity != SeverityError {
			t.Errorf("issue %+v: severity = %q, want %q", iss, iss.Severity, SeverityError)
		}
		if iss.Message == "" {
			t.Errorf("issue for path %q has empty message", iss.Path)
		}
	}
}

func TestValidateUserTemplateIssues_MissingRequiredTopLevel(t *testing.T) {
	// No image, no target: both required at the document root.
	issues := ValidateUserTemplateIssues(toJSON(t, `metadata: {}`))
	if len(issues) == 0 {
		t.Fatal("expected issues for missing required fields, got none")
	}
	found := false
	for _, iss := range issues {
		if iss.Path == "" { // whole-document "missing properties: 'image', 'target'"
			found = true
		}
	}
	if !found {
		t.Errorf("expected a document-level issue for missing image/target; got %+v", issues)
	}
}

func TestValidateUserTemplateIssues_SemanticFDE(t *testing.T) {
	// Schema-valid, but the FDE semantic check fails: enabled with no passphrase.
	y := `image:
  name: my-image
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
systemConfig:
  fde:
    enabled: true
`
	issues := ValidateUserTemplateIssues(toJSON(t, y))
	got := pathsOf(issues)
	if _, ok := got["systemConfig.fde.passphraseFile"]; !ok {
		t.Errorf("expected FDE passphrase issue; got paths %v", keys(got))
	}
}

// A document that isn't a JSON object at all (a YAML list, or a bare scalar) must
// produce exactly the one issue that describes the real problem. The semantic
// checks unmarshal into a map, so before isJSONObject gated them they each failed
// on their own unmarshal and appended an issue blaming `disk` and
// `systemConfig.fde.passphraseFile` — fields the input never mentioned. In the UI
// those render as inline errors on unrelated form controls.
func TestValidateUserTemplateIssues_NonObjectDocument(t *testing.T) {
	for name, y := range map[string]string{
		"yaml list":    "- a\n- b\n",
		"bare string":  "hello\n",
		"bare number":  "42\n",
		"bare boolean": "true\n",
		// null decodes into a nil map, which isJSONObject must not mistake for an
		// object — otherwise it would slip into the semantic checks.
		"null": "null\n",
	} {
		t.Run(name, func(t *testing.T) {
			issues := ValidateUserTemplateIssues(toJSON(t, y))
			if len(issues) != 1 {
				t.Fatalf("got %d issues, want exactly 1: %+v", len(issues), issues)
			}
			// The single issue is document-wide, so it carries no field path.
			if issues[0].Path != "" {
				t.Errorf("path = %q, want \"\" (document-level)", issues[0].Path)
			}
			for _, iss := range issues {
				if iss.Path == "disk" || iss.Path == "systemConfig.fde.passphraseFile" {
					t.Errorf("issue blames unrelated field %q: %s", iss.Path, iss.Message)
				}
			}
		})
	}
}

func TestPointerToPath(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"/":                       "",
		"/target/os":              "target.os",
		"/disk/partitions/0/type": "disk.partitions.0.type",
		"/a~1b":                   "a/b", // ~1 -> /
		"/a~0b":                   "a~b", // ~0 -> ~
	}
	for in, want := range cases {
		if got := pointerToPath(in); got != want {
			t.Errorf("pointerToPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
