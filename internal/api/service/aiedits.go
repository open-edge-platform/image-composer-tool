// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/provider"
	"gopkg.in/yaml.v3"
)

// TemplateEditor is the AI service SuggestTemplateEdits asks: an
// OpenAI-compatible chat API that answers in JSON matching a schema.
type TemplateEditor interface {
	ChatJSON(ctx context.Context, messages []provider.ChatMessage, name string, schema map[string]any) (string, error)
}

// Template edit operations. The browser checks and applies each one the way
// it applies the same edit made by hand.
const (
	EditAdd        = "add"         // add Package, at Version if set
	EditRemove     = "remove"      // remove Package
	EditSetVersion = "set_version" // pin Package to Version; empty unpins
	// EditPinLatest pins Package to its newest version or, with Package
	// empty, every package from the repository Value names, as the Packages
	// step's "Update to latest version" does for the repository it browses.
	EditPinLatest = "pin_latest"
	// EditAddMatching adds every package whose name matches the pattern in
	// Package (* and ? wildcards, such as gir*), from the repository Value
	// names in the user's words or, with Value empty, from every repository
	// in use. The browser expands the pattern against the repositories.
	EditAddMatching  = "add_matching"
	EditSetDiskSize  = "set_disk_size"  // disk size Value, such as 32GiB
	EditSetImageName = "set_image_name" // image name Value
)

// TemplateEdit is one change the AI suggests.
type TemplateEdit struct {
	Op      string `json:"op"`
	Package string `json:"package"`
	Version string `json:"version"`
	Value   string `json:"value"`
}

// TemplateEditSuggestion is the AI's answer: the edits, a short note for the
// user, and what was asked for that the edits can't express.
type TemplateEditSuggestion struct {
	Note        string         `json:"note"`
	Edits       []TemplateEdit `json:"edits"`
	Unsupported []string       `json:"unsupported"`
}

const (
	maxEditRequest    = 2000
	maxEditTemplate   = 1 << 20
	maxEditChanges    = 64 << 10
	maxSuggestedEdits = 50
	aiEditTimeout     = 2 * time.Minute
)

// packageNameRE accepts Debian and RPM package names; anything else in a
// package field is not a package name.
var packageNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+._~-]*$`)

// packagePatternRE accepts a package name pattern: a literal start of at
// least two characters, then at least one * or ? wildcard. The literal start
// is what the browser searches by.
var packagePatternRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+._~-]+[*?][A-Za-z0-9+._~*?-]*$`)

// templateEditSchema is the JSON the model must answer with. Strict
// json_schema needs every property required, so unused fields are empty
// strings.
var templateEditSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []string{"note", "edits", "unsupported"},
	"properties": map[string]any{
		"note": map[string]any{"type": "string"},
		"edits": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"op", "package", "version", "value"},
				"properties": map[string]any{
					"op": map[string]any{
						"type": "string",
						"enum": []string{EditAdd, EditRemove, EditSetVersion, EditPinLatest, EditAddMatching, EditSetDiskSize, EditSetImageName},
					},
					"package": map[string]any{"type": "string"},
					"version": map[string]any{"type": "string"},
					"value":   map[string]any{"type": "string"},
				},
			},
		},
		"unsupported": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
}

const templateEditPrompt = `You edit OS image templates for Image Composer Tool (ICT).
The user describes changes to the template they are editing. Answer only with these edits:
- add: add the package named in "package". Leave "version" empty unless the user gave one.
- remove: remove the package named in "package".
- set_version: pin "package" to "version"; an empty "version" unpins it.
- pin_latest: pin "package" to its newest version; the editor looks the version up. With "package" empty, pin every package that comes from one repository, named in "value" in the user's words (such as "ppa", "intel" or part of its URL).
- add_matching: add every package whose name matches the pattern in "package", with * and ? as wildcards (such as "gir*" or "libva*"); the editor finds the matching packages. If the user names a repository, put it in "value" in their words. If they ask for every repository, set "value" to "all". Otherwise leave "value" empty: the editor then uses the repository the user is browsing, if any, or else every repository in use. Use this whenever the user asks for packages by a name pattern or prefix instead of naming each one.
- set_disk_size: set the disk size to "value", such as "32GiB".
- set_image_name: set the image name to "value".
Use real package names for the template's distribution, preferably from its own repositories. Never invent versions.
Leave fields an edit doesn't use as empty strings.
List anything you can't express with these edits in "unsupported", one short sentence each.
In "note", tell the user in one or two short sentences what you changed.
If the user asks a question instead of asking for changes, such as what has changed so far or what a package is, answer it in "note" (a few short lines; lists are fine), and return no edits and nothing in "unsupported".
"Changes so far" lists how the template differs from the curated one it is based on; the template shown already includes them.`

// SuggestTemplateEdits asks the AI service for edits to templateYAML that do
// what request says. The edits are not checked against any repository here:
// the browser checks them against the repositories the user has checked, as
// it does for edits made by hand.
//
// changes is the user's changes so far as plain text, as the Changes rail
// lists them, so the AI can answer questions about them. It may be empty.
//
// browsing names the repository the user is browsing on the Packages step, or
// is empty; an add_matching edit with no repository of its own uses it.
func (s *Service) SuggestTemplateEdits(ctx context.Context, templateYAML, request, changes, browsing string) (*TemplateEditSuggestion, error) {
	if !s.aiEnabled() {
		return nil, newError(http.StatusForbidden, "FORBIDDEN", "the AI assistant is not turned on for this server")
	}
	request = strings.TrimSpace(request)
	if request == "" {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST", "say what to change")
	}
	if len(request) > maxEditRequest {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST",
			fmt.Sprintf("the request is longer than %d characters", maxEditRequest))
	}
	if len(changes) > maxEditChanges {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST", "the list of changes is too long")
	}
	if strings.TrimSpace(templateYAML) == "" || len(templateYAML) > maxEditTemplate {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST", "the template is empty or too large")
	}
	redacted, err := redactTemplate(templateYAML)
	if err != nil {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST", "the template is not valid YAML: "+err.Error())
	}

	ctx, cancel := context.WithTimeout(ctx, aiEditTimeout)
	defer cancel()
	if strings.TrimSpace(changes) == "" {
		changes = "none"
	}
	answer, err := s.cfg.AI.ChatJSON(ctx, []provider.ChatMessage{
		{Role: "system", Content: templateEditPrompt},
		{Role: "user", Content: "Template:\n```yaml\n" + redacted + "\n```\n\nChanges so far:\n" + changes + browsingLine(browsing) + "\n\nRequest: " + request},
	}, "template_edits", templateEditSchema)
	if err != nil {
		return nil, s.aiError(err)
	}

	var out TemplateEditSuggestion
	if err := json.Unmarshal([]byte(answer), &out); err != nil {
		return nil, newError(http.StatusBadGateway, "AI_BAD_RESPONSE",
			"the AI service answered in an unexpected format; try again")
	}
	return cleanSuggestion(out), nil
}

// browsingLine tells the AI which repository the user is browsing, if any.
func browsingLine(browsing string) string {
	if browsing = strings.TrimSpace(browsing); browsing == "" || strings.ContainsAny(browsing, "\n\r") || len(browsing) > 300 {
		return ""
	}
	return "\n\nRepository the user is browsing: " + browsing
}

// aiError turns an AI service failure into the error the UI shows.
func (s *Service) aiError(err error) error {
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) && apiErr.Unauthorized() {
		msg := "the AI service rejected the token; it may have expired"
		if s.cfg.AITokenURL != "" {
			msg += ". Get a new one at " + s.cfg.AITokenURL
		}
		return newError(http.StatusBadGateway, "AI_UNAUTHORIZED", msg)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newError(http.StatusGatewayTimeout, "AI_UNAVAILABLE", "the AI service took too long to answer; try again")
	}
	return newError(http.StatusBadGateway, "AI_UNAVAILABLE", "the AI service failed: "+err.Error())
}

// cleanSuggestion drops edits the browser couldn't apply, saying so in
// Unsupported rather than silently.
func cleanSuggestion(in TemplateEditSuggestion) *TemplateEditSuggestion {
	out := &TemplateEditSuggestion{Note: strings.TrimSpace(in.Note), Edits: []TemplateEdit{}, Unsupported: []string{}}
	for _, u := range in.Unsupported {
		if u = strings.TrimSpace(u); u != "" {
			out.Unsupported = append(out.Unsupported, u)
		}
	}
	dropped := 0
	for _, e := range in.Edits {
		e = TemplateEdit{
			Op:      e.Op,
			Package: strings.TrimSpace(e.Package),
			Version: strings.TrimSpace(e.Version),
			Value:   strings.TrimSpace(e.Value),
		}
		if !validEdit(e) || len(out.Edits) == maxSuggestedEdits {
			dropped++
			continue
		}
		out.Edits = append(out.Edits, e)
	}
	if dropped > 0 {
		out.Unsupported = append(out.Unsupported, fmt.Sprintf("Skipped %d suggested edit(s) the editor can't apply.", dropped))
	}
	return out
}

func validEdit(e TemplateEdit) bool {
	switch e.Op {
	case EditAdd, EditRemove, EditSetVersion:
		return packageNameRE.MatchString(e.Package) && !strings.ContainsAny(e.Version, " \t\n")
	case EditPinLatest:
		if e.Package != "" {
			return packageNameRE.MatchString(e.Package)
		}
		return e.Value != "" && !strings.ContainsAny(e.Value, "\n")
	case EditAddMatching:
		return packagePatternRE.MatchString(e.Package) && !strings.ContainsAny(e.Value, "\n")
	case EditSetDiskSize, EditSetImageName:
		return e.Value != "" && !strings.ContainsAny(e.Value, "\n")
	}
	return false
}

// redactedKey matches template fields that hold secrets: user passwords and
// password hashes, SSH keys, and any token or secret.
var redactedKey = regexp.MustCompile(`(?i)password$|^sshAuthorizedKeys|token|secret`)

// redactTemplate replaces the values of secret fields with "<removed>" before
// the template leaves the server.
func redactTemplate(text string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", err
	}
	redactNode(&doc)
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	_ = enc.Close()
	return b.String(), nil
}

func redactNode(n *yaml.Node) {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if redactedKey.MatchString(n.Content[i].Value) {
				n.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "<removed>"}
				continue
			}
			redactNode(n.Content[i+1])
		}
		return
	}
	for _, c := range n.Content {
		redactNode(c)
	}
}
