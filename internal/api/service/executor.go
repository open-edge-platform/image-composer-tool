// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// Executor runs a build somewhere other than this machine's ICT binary.
//
// The zero configuration (Config.Executor == nil) is the local behaviour:
// runBuild spawns the ICT CLI and streams its stdout. An Executor instead
// dispatches the build elsewhere — a worker farm reached over the remote build
// protocol (internal/api/executor/remote), for example — so POST /builds and
// its SSE log stream behave identically whichever backend is configured and
// the frontend needs no branch.
//
// This interface is the only seam between the service and a build backend.
// Keeping it here, rather than forking runBuild per backend, is what lets a
// deployment plug in its own backend without patching this package.
//
// docs/user-guide/architecture/web-ui-build-executors.md walks through this
// seam with class and sequence diagrams.
type Executor interface {
	// Name identifies the executor in the startup banner and in build details.
	Name() string

	// Run drives one build to completion and reports how it ended. It must
	// append log lines through h as they arrive, not in a batch at the end, or
	// the SSE stream will appear hung for the life of the build.
	//
	// Returning a non-nil error means the build could not be run at all; a
	// build that ran and failed should return StatusFailed with a nil error so
	// its logs and partial artifacts are preserved.
	Run(ctx context.Context, h ExecutorHandle) (BuildStatus, []Artifact, error)

	// Cancel asks the executor to stop the build. It may return before the
	// build has actually stopped; Run is expected to return soon afterwards.
	Cancel(ctx context.Context, h ExecutorHandle) error
}

// Phase is one step of a build as an Executor reports it. ID is stable and
// machine-readable; Label is what the UI shows.
type Phase struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PhaseReporter is an Executor that knows the ordered phases its builds go
// through, so the UI can draw its stepper from them instead of from the fixed,
// log-derived local phases in phases.go.
type PhaseReporter interface {
	Phases() []Phase
}

// Recovered is a past build read back from where it ran.
type Recovered struct {
	Status    BuildStatus
	ErrMsg    string
	Log       []string
	Artifacts []Artifact
	// Meta is the executor detail GET /builds/{id}/details shows (worker, URL).
	Meta map[string]string
}

// ErrStillRunning is returned by Recover for a build that has not finished yet.
var ErrStillRunning = errors.New("build is still running")

// Recoverer is an Executor that can read a build back from where it ran, by the
// id this service gave it. A restarted server uses it for a build whose record
// says it was running when the previous instance went away.
type Recoverer interface {
	Recover(ctx context.Context, buildID string) (Recovered, error)
}

// ExecutorHandle is an executor's view of a single build. It deliberately exposes
// no way to set the terminal status: only the value Run returns decides that,
// so there is one path to completion rather than two that can disagree.
type ExecutorHandle interface {
	ID() string
	// TemplatePath is the on-disk template this build runs.
	TemplatePath() string
	// TemplateYAML is that template's contents, self-contained (no extends),
	// which remote executors send to the worker rather than a path the worker
	// cannot read.
	TemplateYAML() (string, error)
	// WorkDir is the per-build directory for outputs.
	WorkDir() string
	// AppendLog publishes one line to the build log and hence to SSE.
	AppendLog(line string)
	// SetPhase reports which phase the build is in; the SSE stream forwards it.
	SetPhase(p Phase)
	// PutMeta records backend-specific detail — worker name, job URL — for
	// GET /builds/{id}/details.
	PutMeta(key, value string)
}

// executorHandle adapts *build to ExecutorHandle.
type executorHandle struct {
	b *build
}

func (h executorHandle) ID() string            { return h.b.ID }
func (h executorHandle) TemplatePath() string  { return h.b.TemplatePath }
func (h executorHandle) WorkDir() string       { return h.b.WorkDir }
func (h executorHandle) AppendLog(line string) { h.b.appendLog(line) }
func (h executorHandle) SetPhase(p Phase)      { h.b.setPhase(p) }
func (h executorHandle) PutMeta(k, v string)   { h.b.putMeta(k, v) }

func (h executorHandle) TemplateYAML() (string, error) {
	if h.b.DeltaPath != "" {
		return selfContainedDelta(h.b.DeltaPath)
	}
	data, err := os.ReadFile(h.b.TemplatePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// selfContainedDelta folds a generated delta into the curated template it
// extends, so a worker builds this host's template rather than resolving the
// extends path against its own checkout (which may lack the template or hold a
// different version). OS defaults are left out: the worker's ICT applies them.
// Host paths under the templates directory are made relative to it again,
// because the worker writes the file at the root of its own templates directory.
func selfContainedDelta(deltaPath string) (string, error) {
	merged, _, err := config.ResolveAndMergeExtendsChain(deltaPath, nil)
	if err != nil {
		return "", fmt.Errorf("resolving the build's template: %w", err)
	}
	merged.Extends = ""
	data, err := config.MarshalTemplateYAML(merged)
	if err != nil {
		return "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	pruneEmpty(&doc)
	if data, err = yaml.Marshal(&doc); err != nil {
		return "", err
	}
	if yaml.Unmarshal(data, &map[string]any{}) != nil {
		data = invalidBlockScalarHeader.ReplaceAll(data, []byte("$1$2\n"))
	}
	root, err := filepath.Abs(filepath.Dir(deltaPath))
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(data), root+string(filepath.Separator), ""), nil
}

// invalidBlockScalarHeader matches the indent indicators yaml.v3 can emit
// invalidly ("|4-"); config.MarshalTemplateYAML applies the same fix.
var invalidBlockScalarHeader = regexp.MustCompile(`(: \|)\d+([+-]?)\n`)

// pruneEmpty drops mapping entries whose value is an empty string, an empty
// list, or a map left empty after pruning. The config structs marshal every
// unset field, and some of those zero values (an empty bootType) are not valid
// template input; ICT reads an absent field and an empty one alike.
func pruneEmpty(n *yaml.Node) bool {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			pruneEmpty(c)
		}
		return false
	case yaml.MappingNode:
		kept := n.Content[:0]
		for i := 0; i+1 < len(n.Content); i += 2 {
			if !pruneEmpty(n.Content[i+1]) {
				kept = append(kept, n.Content[i], n.Content[i+1])
			}
		}
		n.Content = kept
		return len(kept) == 0
	case yaml.SequenceNode:
		kept := n.Content[:0]
		for _, c := range n.Content {
			if c.Kind != yaml.MappingNode || !pruneEmpty(c) {
				kept = append(kept, c)
			}
		}
		n.Content = kept
		return len(kept) == 0
	case yaml.ScalarNode:
		return n.Tag == "!!null" || (n.Tag == "!!str" && n.Value == "")
	}
	return false
}

// runViaExecutor drives one build through the configured Executor and records
// the same terminal state the local path would: status, artifacts, log file.
//
// It is deliberately the only place that knows an executor exists, so the rest
// of the service — history, SSE, details, cancel bookkeeping — is shared by both
// backends rather than duplicated per backend.
func (s *Service) runViaExecutor(b *build) {
	log := logger.Logger()
	ex := s.cfg.Executor
	b.putMeta("executor", ex.Name())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.setExecutorCancel(b.ID, cancel)
	defer s.clearExecutorCancel(b.ID)

	// Without this the build reports not-started for its whole life: the local
	// path leaves not-started only when it records the child's process group,
	// and an executor never has one.
	b.markRunning()

	status, arts, err := ex.Run(ctx, executorHandle{b: b})
	// The local path removes the generated delta once the child has exited;
	// the executor path has no child, so this is its equivalent point.
	b.archiveAndCleanupDelta()

	var errMsg string
	if err != nil {
		errMsg = err.Error()
		// A build that ran and failed reports StatusFailed with a nil error; a
		// non-nil error means it could not be run at all. Only override the status
		// when the executor left it unset, so a cancelled build stays cancelled.
		if status == "" {
			status = StatusFailed
		}
		log.Warnf("build %s: %s executor: %v", b.ID, ex.Name(), err)
	}
	if status == "" {
		status = StatusFailed
		errMsg = "executor returned no status"
	}
	if b.wasCancelRequested() && status == StatusFailed {
		// A cancel that lands mid-build usually surfaces as a non-zero exit; prefer
		// the user's intent over the symptom, matching classifyExit on the local path.
		status = StatusCancelled
	}

	b.finish(status, arts, errMsg)
}

// setExecutorCancel records the cancel func for an in-flight executor build so
// CancelBuild can stop it. The local path signals a process group instead.
func (s *Service) setExecutorCancel(id string, cancel context.CancelFunc) {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	if s.executorCancels == nil {
		s.executorCancels = make(map[string]context.CancelFunc)
	}
	s.executorCancels[id] = cancel
}

func (s *Service) clearExecutorCancel(id string) {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	delete(s.executorCancels, id)
}

func (s *Service) executorCancel(id string) context.CancelFunc {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	return s.executorCancels[id]
}

// RecoverExecutorBuilds brings every build the previous instance recorded as
// still running to its real state, when the configured Executor can report it
// (see Recoverer). It is a no-op for the local path and for executors that
// cannot recover. Called once, before the server starts serving.
func (s *Service) RecoverExecutorBuilds(ctx context.Context) {
	s.recoverExecutorBuilds(ctx, 30*time.Second)
}

// recoverExecutorBuilds asks a Recoverer executor about every build this server
// recorded as still running before it last stopped, and brings each record to
// its real terminal state. Builds the executor still reports as running are
// re-attached: their log is followed to completion as if Run had never been
// interrupted. Called once at startup; a no-op for other executors.
func (s *Service) recoverExecutorBuilds(ctx context.Context, timeout time.Duration) {
	rec, ok := s.cfg.Executor.(Recoverer)
	if !ok {
		return
	}
	log := logger.Logger()
	for _, b := range s.listRunningOnDisk() {
		rctx, cancel := context.WithTimeout(ctx, timeout)
		got, err := rec.Recover(rctx, b.ID)
		cancel()
		switch {
		case errors.Is(err, ErrStillRunning):
			// Re-adopt it: Run follows the executor's stream from the start, so
			// the log buffer is complete for late SSE subscribers.
			if ok, _, _ := s.tryAcquireBuildSlot(b.ID); !ok {
				log.Warnf("build %s: still running on %s but no slot is free to follow it", b.ID, s.cfg.Executor.Name())
				continue
			}
			s.endBuildSetup()
			s.tracker.add(b)
			log.Infof("build %s: re-attached to %s", b.ID, s.cfg.Executor.Name())
			go s.runBuild(b, "", nil)
		case err != nil:
			log.Warnf("build %s: recovering from %s: %v", b.ID, s.cfg.Executor.Name(), err)
			b.appendLog("ERROR server restarted while this build was running and it could not be recovered: " +
				err.Error())
			b.finish(StatusFailed, nil, "server restarted; build outcome unknown: "+err.Error())
		default:
			for _, line := range got.Log {
				b.appendLog(line)
			}
			for k, v := range got.Meta {
				b.putMeta(k, v)
			}
			b.finish(got.Status, got.Artifacts, got.ErrMsg)
			log.Infof("build %s: recovered from %s as %s", b.ID, s.cfg.Executor.Name(), got.Status)
		}
	}
}
