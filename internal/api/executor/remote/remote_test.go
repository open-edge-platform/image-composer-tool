// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// handle is a test ExecutorHandle that records what the executor reports.
type handle struct {
	mu    sync.Mutex
	id    string
	yaml  string
	lines []string
	phase service.Phase
	meta  map[string]string
}

func newHandle(id, yaml string) *handle {
	return &handle{id: id, yaml: yaml, meta: map[string]string{}}
}

func (h *handle) ID() string                    { return h.id }
func (h *handle) TemplatePath() string          { return "" }
func (h *handle) TemplateYAML() (string, error) { return h.yaml, nil }
func (h *handle) WorkDir() string               { return "" }
func (h *handle) AppendLog(line string)         { h.mu.Lock(); h.lines = append(h.lines, line); h.mu.Unlock() }
func (h *handle) SetPhase(p service.Phase)      { h.mu.Lock(); h.phase = p; h.mu.Unlock() }
func (h *handle) PutMeta(k, v string)           { h.mu.Lock(); h.meta[k] = v; h.mu.Unlock() }
func (h *handle) logText() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.lines, "\n")
}
func (h *handle) currentPhase() service.Phase { h.mu.Lock(); defer h.mu.Unlock(); return h.phase }
func (h *handle) metaValue(k string) string   { h.mu.Lock(); defer h.mu.Unlock(); return h.meta[k] }
func (h *handle) lineCount() int              { h.mu.Lock(); defer h.mu.Unlock(); return len(h.lines) }

const tmpl = "image:\n  name: demo\n  version: \"1\"\n" +
	"target:\n  os: ubuntu\n  dist: ubuntu24\n  arch: x86_64\n  imageType: iso\n"

func newExecutor(t *testing.T, f *fakeService, mut func(*Config)) *Executor {
	t.Helper()
	cfg := Config{URL: f.URL(), CancelGrace: 2 * time.Second, ReconnectMax: 5 * time.Second}
	if mut != nil {
		mut(&cfg)
	}
	e, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNewReadsInfoAndRefusesOtherProtocols(t *testing.T) {
	f := newFakeService()
	defer f.Close()
	e := newExecutor(t, f, nil)
	if e.Name() != "fake-farm" || e.MaxConcurrent() != 3 || len(e.Phases()) != 1 {
		t.Fatalf("info not applied: %+v", e.info)
	}
	f.mu.Lock()
	f.info.Protocol = "2"
	f.mu.Unlock()
	_, err := New(context.Background(), Config{URL: f.URL()})
	if err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("New accepted protocol 2: %v", err)
	}
}

func TestRunSubmitsFollowsAndReturnsOutcome(t *testing.T) {
	f := newFakeService()
	defer f.Close()
	e := newExecutor(t, f, func(c *Config) { c.Labels = map[string]string{"environment": "dev"} })
	h := newHandle("b1", tmpl)

	type out struct {
		st   service.BuildStatus
		arts []service.Artifact
		err  error
	}
	res := make(chan out, 1)
	go func() {
		st, arts, err := e.Run(context.Background(), h)
		res <- out{st, arts, err}
	}()

	waitFor(t, "submission", func() bool { return f.get("b1") != nil })
	f.mu.Lock()
	sub := f.submissions[0]
	f.mu.Unlock()
	if sub.Labels["environment"] != "dev" || sub.Labels["image"] != "demo" || sub.Template != tmpl {
		t.Errorf("submission = %+v", sub)
	}

	f.meta("b1", "worker", "worker-03")
	f.phase("b1", "dispatching", "Preparing infra")
	f.log("b1", "line one", "line two")
	waitFor(t, "lines", func() bool { return h.lineCount() >= 3 }) // the "submitted" line plus two
	if p := h.currentPhase(); p.ID != "dispatching" || p.Label != "Preparing infra" {
		t.Errorf("phase = %+v", p)
	}
	if h.metaValue("worker") != "worker-03" {
		t.Errorf("meta = %v", h.meta)
	}
	f.finish("b1", "succeeded", "", artifact{
		Name: "demo.iso", Type: "image", URL: "https://store/demo.iso", SizeBytes: 1_130_000_000,
		SHA256: strings.Repeat("a", 64),
	})

	select {
	case o := <-res:
		if o.err != nil || o.st != service.StatusSuccess || len(o.arts) != 1 {
			t.Fatalf("Run = %+v", o)
		}
		a := o.arts[0]
		if a.URL != "https://store/demo.iso" || a.Size != "1.13 GB" || a.SHA256 == "" || a.Type != "image" {
			t.Errorf("artifact = %+v", a)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
	if !strings.Contains(h.logText(), "line two") {
		t.Errorf("log = %q", h.logText())
	}
}

func TestRunResumesABrokenStreamWithoutDuplicatingLines(t *testing.T) {
	f := newFakeService()
	defer f.Close()
	f.dropStreamAfter = 2
	e := newExecutor(t, f, nil)
	h := newHandle("b2", tmpl)
	done := make(chan service.BuildStatus, 1)
	go func() {
		st, _, _ := e.Run(context.Background(), h)
		done <- st
	}()
	waitFor(t, "submission", func() bool { return f.get("b2") != nil })
	f.log("b2", "l0", "l1", "l2", "l3", "l4")
	waitFor(t, "all lines", func() bool { return h.lineCount() >= 6 })
	f.finish("b2", "failed", "exit status 1")
	select {
	case st := <-done:
		if st != service.StatusFailed {
			t.Fatalf("status = %s", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	got := h.logText()
	for _, l := range []string{"l0", "l1", "l2", "l3", "l4"} {
		if strings.Count(got, l+"\n") > 1 || !strings.Contains(got, l) {
			t.Errorf("line %s appears %d times in %q", l, strings.Count(got, l), got)
		}
	}
}

func TestCancelAsksTheServiceAndRunReportsTheRealOutcome(t *testing.T) {
	f := newFakeService()
	defer f.Close()
	e := newExecutor(t, f, nil)
	h := newHandle("b3", tmpl)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan service.BuildStatus, 1)
	go func() {
		st, _, _ := e.Run(ctx, h)
		done <- st
	}()
	waitFor(t, "submission", func() bool { return f.get("b3") != nil })
	f.log("b3", "working")
	if err := e.Cancel(context.Background(), h); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	cancel() // what the service does right after Executor.Cancel
	f.finish("b3", "cancelled", "")
	select {
	case st := <-done:
		if st != service.StatusCancelled {
			t.Fatalf("status = %s", st)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cancels) != 1 || f.cancels[0] != "b3" {
		t.Errorf("cancels = %v", f.cancels)
	}
}

func TestCancelGraceGivesUpOnASilentService(t *testing.T) {
	f := newFakeService()
	defer f.Close()
	e := newExecutor(t, f, func(c *Config) { c.CancelGrace = 300 * time.Millisecond })
	h := newHandle("b4", tmpl)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan service.BuildStatus, 1)
	go func() {
		st, _, _ := e.Run(ctx, h)
		done <- st
	}()
	waitFor(t, "submission", func() bool { return f.get("b4") != nil })
	cancel()
	select {
	case st := <-done:
		if st != service.StatusCancelled {
			t.Fatalf("status = %s", st)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not give up after the cancel grace")
	}
}

func TestRecover(t *testing.T) {
	f := newFakeService()
	defer f.Close()
	e := newExecutor(t, f, nil)
	f.seed("old", "succeeded", "a", "b")
	f.finish("old", "succeeded", "", artifact{Name: "x.iso", Type: "image", URL: "https://store/x.iso"})
	f.seed("live", "running", "still going")

	rec, err := e.Recover(context.Background(), "old")
	if err != nil || rec.Status != service.StatusSuccess || len(rec.Log) != 2 || len(rec.Artifacts) != 1 {
		t.Fatalf("Recover(old) = %+v, %v", rec, err)
	}
	if _, err := e.Recover(context.Background(), "live"); err != service.ErrStillRunning {
		t.Fatalf("Recover(live) err = %v, want ErrStillRunning", err)
	}
	if _, err := e.Recover(context.Background(), "nope"); err == nil {
		t.Fatal("Recover(nope) succeeded")
	}
}

func TestBearerTokenIsSent(t *testing.T) {
	f := newFakeService()
	defer f.Close()
	f.token = "s3cret"
	if _, err := New(context.Background(), Config{URL: f.URL()}); err == nil {
		t.Fatal("New without a token succeeded against a service that requires one")
	}
	e := newExecutor(t, f, func(c *Config) { c.Token = "s3cret" })
	if e.Name() != "fake-farm" {
		t.Fatalf("name = %q", e.Name())
	}
}

func TestReferencedFilesAreShipped(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "additionalfiles"), 0o755))
	must(os.MkdirAll(filepath.Join(dir, "initrd"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "additionalfiles", "setup.sh"), []byte("#!/bin/sh\n"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "initrd", "pin.yml"), []byte("image: {}\n"), 0o644))
	f := newFakeService()
	defer f.Close()
	e := newExecutor(t, f, func(c *Config) { c.TemplatesDir = dir })
	withFiles := tmpl + "systemConfig:\n  name: x\n  initramfs:\n    template: initrd/pin.yml\n" +
		"  additionalFiles:\n    - local: additionalfiles/setup.sh\n      final: /opt/setup.sh\n" +
		"    - local: ../../escape\n      final: /x\n    - local: additionalfiles/missing\n      final: /y\n"
	files := e.referencedFiles(withFiles)
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	byPath := map[string]file{}
	for _, fl := range files {
		byPath[fl.Path] = fl
	}
	sh, ok := byPath["additionalfiles/setup.sh"]
	if !ok || sh.Mode != "0755" {
		t.Errorf("setup.sh = %+v", sh)
	}
	data, _ := base64.StdEncoding.DecodeString(sh.ContentBase64)
	if string(data) != "#!/bin/sh\n" {
		t.Errorf("content = %q", data)
	}
	if _, ok := byPath["initrd/pin.yml"]; !ok {
		t.Errorf("initrd template not shipped: %v", byPath)
	}
}
