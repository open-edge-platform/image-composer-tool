// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeExecutor is an Executor whose Run blocks until released, so tests can
// observe a build mid-flight, and whose outcome the test chooses.
type fakeExecutor struct {
	mu        sync.Mutex
	release   chan struct{}
	status    BuildStatus
	artifacts []Artifact
	err       error
	cancelled []string
	templates map[string]string
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		release:   make(chan struct{}),
		status:    StatusSuccess,
		templates: map[string]string{},
	}
}

func (f *fakeExecutor) Name() string { return "fake-farm" }

func (f *fakeExecutor) Phases() []Phase {
	return []Phase{{ID: "dispatching", Label: "Preparing infra"}, {ID: "building", Label: "Building"}}
}

func (f *fakeExecutor) Run(ctx context.Context, h ExecutorHandle) (BuildStatus, []Artifact, error) {
	yaml, err := h.TemplateYAML()
	if err != nil {
		return "", nil, err
	}
	f.mu.Lock()
	f.templates[h.ID()] = yaml
	f.mu.Unlock()
	h.PutMeta("worker", "worker-07")
	h.SetPhase(Phase{ID: "dispatching", Label: "Preparing infra"})
	h.AppendLog("[dispatcher] picked worker-07")
	select {
	case <-f.release:
	case <-ctx.Done():
		return StatusCancelled, nil, nil
	}
	h.SetPhase(Phase{ID: "building", Label: "Building"})
	h.AppendLog("done")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.artifacts, f.err
}

func (f *fakeExecutor) Cancel(_ context.Context, h ExecutorHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, h.ID())
	return nil
}

func newExecutorService(t *testing.T, ex Executor, maxConcurrent int) *Service {
	t.Helper()
	s := newTestService(t)
	s.cfg.Executor = ex
	s.cfg.MaxConcurrent = maxConcurrent
	return s
}

func composeRequest() BuildRequest {
	return BuildRequest{Compose: &Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
	}}
}

func waitDone(t *testing.T, s *Service, id string) Result {
	t.Helper()
	h, ok := s.Build(id)
	if !ok {
		t.Fatalf("build %s not found", id)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("build %s did not finish", id)
	}
	return h.Result()
}

// slotsHeld reports how many build slots are held, waiting briefly for the
// release that runBuild defers until after done is closed.
func slotsHeld(s *Service) int {
	deadline := time.Now().Add(time.Second)
	for {
		s.buildMu.Lock()
		n := len(s.activeBuilds)
		s.buildMu.Unlock()
		if n == 0 || time.Now().After(deadline) {
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The executor path must leave the rest of the service untouched: the build is
// tracked, running, streams its log, reports the executor's phase and detail,
// and ends with the status and artifacts the executor returned.
func TestExecutorRunsBuildAndRecordsOutcome(t *testing.T) {
	ex := newFakeExecutor()
	ex.artifacts = []Artifact{{Name: "img.raw.gz", Type: "image", URL: "https://store/img.raw.gz", SHA256: "ab"}}
	s := newExecutorService(t, ex, 1)

	acc, err := s.StartBuild(composeRequest())
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	h, _ := s.Build(acc.BuildID)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if p, ok := h.ReportedPhase(); ok && p.ID == "dispatching" && h.Result().Status == StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("build never reported the executor's phase; status %s", h.Result().Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(strings.Join(h.SnapshotLogs(), "\n"), "picked worker-07") {
		t.Errorf("executor log line not streamed: %q", h.SnapshotLogs())
	}
	details, _ := s.BuildDetails(acc.BuildID)
	if details.Executor != "fake-farm" || details.Meta["worker"] != "worker-07" {
		t.Errorf("details executor=%q meta=%v", details.Executor, details.Meta)
	}

	close(ex.release)
	res := waitDone(t, s, acc.BuildID)
	if res.Status != StatusSuccess || len(res.Artifacts) != 1 || res.Artifacts[0].URL == "" {
		t.Fatalf("result = %+v", res)
	}

	// The record on disk carries the executor detail, so a restart still shows it.
	data, err := os.ReadFile(filepath.Join(s.buildsRoot(), acc.BuildID, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m buildMeta
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Meta["worker"] != "worker-07" || m.Meta["executor"] != "fake-farm" {
		t.Errorf("persisted meta = %v", m.Meta)
	}
	if len(m.Artifacts) != 1 || m.Artifacts[0].URL != "https://store/img.raw.gz" {
		t.Errorf("persisted artifacts = %+v", m.Artifacts)
	}
	if n := slotsHeld(s); n != 0 {
		t.Errorf("%d slots still held after the build finished", n)
	}
}

// A worker-side executor can run several builds at once; the slot count follows
// MaxConcurrent, and the limit is still enforced.
func TestExecutorHonoursMaxConcurrent(t *testing.T) {
	ex := newFakeExecutor()
	s := newExecutorService(t, ex, 2)
	if s.MaxConcurrent() != 2 {
		t.Fatalf("MaxConcurrent = %d", s.MaxConcurrent())
	}
	for i := 0; i < 2; i++ {
		if _, err := s.StartBuild(composeRequest()); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	_, err := s.StartBuild(composeRequest())
	var serr *Error
	if !errors.As(err, &serr) || serr.Code != "BUILD_IN_PROGRESS" {
		t.Fatalf("third start: err = %v, want BUILD_IN_PROGRESS", err)
	}
	close(ex.release)
	for _, b := range s.tracker.all() {
		waitDone(t, s, b.ID)
	}
}

// Cancel asks the executor to stop and cancels Run's context; the outcome the
// executor then returns is what the build records.
func TestExecutorCancel(t *testing.T) {
	ex := newFakeExecutor()
	s := newExecutorService(t, ex, 1)
	acc, err := s.StartBuild(composeRequest())
	if err != nil {
		t.Fatal(err)
	}
	h, _ := s.Build(acc.BuildID)
	for h.Result().Status != StatusRunning {
		time.Sleep(5 * time.Millisecond)
	}
	ca, err := s.CancelBuild(acc.BuildID)
	if err != nil || ca.Status != StatusCancelling {
		t.Fatalf("CancelBuild = %+v, %v", ca, err)
	}
	res := waitDone(t, s, acc.BuildID)
	if res.Status != StatusCancelled {
		t.Fatalf("status = %s, want cancelled", res.Status)
	}
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if len(ex.cancelled) != 1 || ex.cancelled[0] != acc.BuildID {
		t.Errorf("executor.Cancel called for %v", ex.cancelled)
	}
}

// An executor that could not run the build at all fails it with the error.
func TestExecutorRunErrorFailsBuild(t *testing.T) {
	ex := newFakeExecutor()
	ex.err = errors.New("farm unreachable")
	ex.status = ""
	close(ex.release)
	s := newExecutorService(t, ex, 1)
	acc, err := s.StartBuild(composeRequest())
	if err != nil {
		t.Fatal(err)
	}
	res := waitDone(t, s, acc.BuildID)
	if res.Status != StatusFailed || !strings.Contains(res.ErrMsg, "farm unreachable") {
		t.Fatalf("result = %+v", res)
	}
}

// The executor receives a self-contained template: the Advanced-tab delta and
// its extends chain folded into one document the worker can build on its own.
func TestExecutorReceivesSelfContainedTemplate(t *testing.T) {
	ex := newFakeExecutor()
	close(ex.release)
	s := newExecutorService(t, ex, 1)
	req := composeRequest()
	req.Compose.Packages = []string{"htop"}
	acc, err := s.StartBuild(req)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, acc.BuildID)
	ex.mu.Lock()
	yaml := ex.templates[acc.BuildID]
	ex.mu.Unlock()
	if strings.Contains(yaml, "extends:") {
		t.Errorf("template still extends another file:\n%s", yaml)
	}
	if !strings.Contains(yaml, "htop") || !strings.Contains(yaml, "name: test-image") {
		t.Errorf("template is not the folded delta:\n%s", yaml)
	}
	if entries, _ := filepath.Glob(filepath.Join(s.cfg.TemplatesDir, ".ict-adv-*")); len(entries) != 0 {
		t.Errorf("delta left behind: %v", entries)
	}
}

// Shutdown must not cancel executor builds: they are not this host's children.
func TestExecutorBuildsSurviveShutdownCancel(t *testing.T) {
	ex := newFakeExecutor()
	s := newExecutorService(t, ex, 1)
	acc, err := s.StartBuild(composeRequest())
	if err != nil {
		t.Fatal(err)
	}
	s.CancelInFlightBuilds(time.Second)
	h, _ := s.Build(acc.BuildID)
	if st := h.Result().Status; st == StatusCancelling || st == StatusCancelled {
		t.Fatalf("executor build was cancelled on shutdown: %s", st)
	}
	close(ex.release)
	waitDone(t, s, acc.BuildID)
}
