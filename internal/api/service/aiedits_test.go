// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/provider"
)

// fakeEditor records what it was asked and answers with answer or err.
type fakeEditor struct {
	answer   string
	err      error
	messages []provider.ChatMessage
	schema   map[string]any
}

func (f *fakeEditor) ChatJSON(_ context.Context, messages []provider.ChatMessage, _ string, schema map[string]any) (string, error) {
	f.messages, f.schema = messages, schema
	return f.answer, f.err
}

const aiTestTemplate = `image:
  name: nvl
systemConfig:
  users:
    - name: user
      password: "$6$secret-hash"
      sshAuthorizedKeys: ["ssh-ed25519 AAAA me@host"]
  packages:
    - snapd
oepInstaller:
  token: ghp_abc
`

func aiService(t *testing.T, e TemplateEditor) *Service {
	s := newTestService(t)
	s.cfg.Authoring = true
	s.cfg.AI = e
	s.cfg.AITokenURL = "https://gnai.example/sso"
	return s
}

func wantCode(t *testing.T, err error, status int, code string) *Error {
	t.Helper()
	var se *Error
	if !errors.As(err, &se) || se.Status != status || se.Code != code {
		t.Fatalf("error = %v, want %d %s", err, status, code)
	}
	return se
}

func TestSuggestTemplateEdits(t *testing.T) {
	f := &fakeEditor{answer: `{"note":"Added docker.io and removed snapd.",
		"edits":[{"op":"add","package":"docker.io","version":"","value":""},
		         {"op":"remove","package":" snapd ","version":"","value":""},
		         {"op":"set_disk_size","package":"","version":"","value":"32GiB"}],
		"unsupported":["Partitions can't be added.", " "]}`}
	got, err := aiService(t, f).SuggestTemplateEdits(context.Background(), aiTestTemplate, "add docker, remove snapd, 32 GiB disk", "Removed: vim")
	if err != nil {
		t.Fatal(err)
	}
	if got.Note != "Added docker.io and removed snapd." || len(got.Edits) != 3 || got.Edits[1].Package != "snapd" {
		t.Errorf("suggestion = %+v", got)
	}
	if len(got.Unsupported) != 1 {
		t.Errorf("unsupported = %q, want the one non-empty entry", got.Unsupported)
	}

	sent := f.messages[len(f.messages)-1].Content
	for _, secret := range []string{"secret-hash", "ssh-ed25519", "ghp_abc"} {
		if strings.Contains(sent, secret) {
			t.Errorf("the request sent %q to the AI service:\n%s", secret, sent)
		}
	}
	for _, kept := range []string{"snapd", "name: user", "add docker, remove snapd", "Changes so far:\nRemoved: vim"} {
		if !strings.Contains(sent, kept) {
			t.Errorf("the request lacks %q:\n%s", kept, sent)
		}
	}
	if f.schema["required"] == nil {
		t.Error("no response schema was sent")
	}
}

func TestSuggestTemplateEditsDropsEditsItCantApply(t *testing.T) {
	f := &fakeEditor{answer: `{"note":"","unsupported":[],"edits":[
		{"op":"add","package":"rm -rf /","version":"","value":""},
		{"op":"partition","package":"","version":"","value":"x"},
		{"op":"set_image_name","package":"","version":"","value":""},
		{"op":"set_version","package":"linux-image-7.3-intel","version":"261014t080608z-resolute1","value":""}]}`}
	got, err := aiService(t, f).SuggestTemplateEdits(context.Background(), aiTestTemplate, "pin the kernel", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Edits) != 1 || got.Edits[0].Op != EditSetVersion {
		t.Errorf("edits = %+v, want only the set_version", got.Edits)
	}
	if len(got.Unsupported) != 1 || !strings.Contains(got.Unsupported[0], "3 suggested edit") {
		t.Errorf("unsupported = %q, want a note about the 3 skipped edits", got.Unsupported)
	}
}

func TestSuggestTemplateEditsErrors(t *testing.T) {
	ctx := context.Background()

	off := newTestService(t)
	off.cfg.AI = &fakeEditor{}
	_, err := off.SuggestTemplateEdits(ctx, aiTestTemplate, "x", "")
	wantCode(t, err, http.StatusForbidden, "FORBIDDEN") // AI needs authoring too

	s := aiService(t, &fakeEditor{})
	_, err = s.SuggestTemplateEdits(ctx, aiTestTemplate, "  ", "")
	wantCode(t, err, http.StatusBadRequest, "BAD_REQUEST")
	_, err = s.SuggestTemplateEdits(ctx, "a: [", "x", "")
	wantCode(t, err, http.StatusBadRequest, "BAD_REQUEST")

	s = aiService(t, &fakeEditor{err: &provider.APIError{StatusCode: http.StatusUnauthorized, Message: "expired"}})
	_, err = s.SuggestTemplateEdits(ctx, aiTestTemplate, "x", "")
	if se := wantCode(t, err, http.StatusBadGateway, "AI_UNAUTHORIZED"); !strings.Contains(se.Message, "https://gnai.example/sso") {
		t.Errorf("message %q does not say where to get a new token", se.Message)
	}

	s = aiService(t, &fakeEditor{answer: "not json"})
	_, err = s.SuggestTemplateEdits(ctx, aiTestTemplate, "x", "")
	wantCode(t, err, http.StatusBadGateway, "AI_BAD_RESPONSE")

	s = aiService(t, &fakeEditor{err: context.DeadlineExceeded})
	_, err = s.SuggestTemplateEdits(ctx, aiTestTemplate, "x", "")
	wantCode(t, err, http.StatusGatewayTimeout, "AI_UNAVAILABLE")
}

func TestSessionAI(t *testing.T) {
	s := aiService(t, &fakeEditor{})
	if c := s.Session(); !c.AI || !c.Authoring {
		t.Errorf("session = %+v, want authoring and ai", c)
	}
	s.cfg.Authoring = false
	if s.Session().AI {
		t.Error("ai without authoring")
	}
}

func TestSuggestTemplateEditsPinLatest(t *testing.T) {
	f := &fakeEditor{answer: `{"note":"","unsupported":[],"edits":[
		{"op":"pin_latest","package":"","version":"","value":"ppa"},
		{"op":"pin_latest","package":"htop","version":"","value":""},
		{"op":"pin_latest","package":"","version":"","value":""}]}`}
	got, err := aiService(t, f).SuggestTemplateEdits(context.Background(), aiTestTemplate, "pin the ppa packages", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Edits) != 2 || got.Edits[0].Value != "ppa" || got.Edits[1].Package != "htop" {
		t.Errorf("edits = %+v, want the repository and the package pin_latest, not the empty one", got.Edits)
	}
	if !strings.Contains(templateEditPrompt, "pin_latest") {
		t.Error("the prompt doesn't offer pin_latest")
	}
}

func TestSuggestTemplateEditsAddMatching(t *testing.T) {
	answer := `{"note":"Adding the gir packages.","unsupported":[],"edits":[` +
		`{"op":"add_matching","package":"gir*","version":"","value":"af01p"},` +
		`{"op":"add_matching","package":"libva*","version":"","value":""},` +
		`{"op":"add_matching","package":"*","version":"","value":""},` +
		`{"op":"add_matching","package":"g*","version":"","value":""},` +
		`{"op":"add_matching","package":"gir","version":"","value":""}]}`
	got, err := aiService(t, &fakeEditor{answer: answer}).SuggestTemplateEdits(context.Background(), aiTestTemplate, "add all gir packages from af01p", "")
	if err != nil {
		t.Fatalf("SuggestTemplateEdits: %v", err)
	}
	// A pattern needs a literal start of two characters and a wildcard.
	if len(got.Edits) != 2 || got.Edits[0].Package != "gir*" || got.Edits[0].Value != "af01p" || got.Edits[1].Package != "libva*" {
		t.Errorf("edits = %+v, want gir* (from af01p) and libva*", got.Edits)
	}
	if len(got.Unsupported) != 1 || !strings.Contains(got.Unsupported[0], "3 suggested edit") {
		t.Errorf("unsupported = %v, want the three bad patterns reported", got.Unsupported)
	}
	if !strings.Contains(templateEditPrompt, "add_matching") {
		t.Error("the prompt doesn't offer add_matching")
	}
}
