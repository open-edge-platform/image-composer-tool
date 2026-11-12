// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
)

// Config configures the remote executor.
type Config struct {
	// URL is the build service's base URL; the client appends /v1/....
	URL string
	// Token, when set, is sent as a bearer token on every request.
	Token string
	// TemplatesDir, when set, lets Run ship the auxiliary files a template
	// references relative to it (additionalFiles[].local, initramfs.template)
	// so the worker does not need to already have them. Empty ships none.
	TemplatesDir string
	// Labels are sent with every submission, e.g. {"environment": "staging"}.
	Labels map[string]string
	// HTTPClient defaults to network.NewSecureHTTPClient(), whose 30s header
	// timeout bounds every call but the event stream, which is read as it comes.
	HTTPClient *http.Client
	// CancelGrace bounds how long Run keeps following a cancelled build for its
	// real outcome before giving up and reporting it cancelled. Default 2m.
	CancelGrace time.Duration
	// ReconnectMax bounds how long Run retries a broken event stream before
	// asking GET /v1/builds/{id} for the outcome and, failing that, giving up.
	// Default 10m.
	ReconnectMax time.Duration
}

// Executor runs builds through a remote build service.
type Executor struct {
	cfg    Config
	client *http.Client
	info   Info
}

var _ service.Executor = (*Executor)(nil)
var _ service.PhaseReporter = (*Executor)(nil)
var _ service.Recoverer = (*Executor)(nil)

// New connects to the build service, checks it speaks this protocol, and
// returns an Executor sized from its Info.
func New(ctx context.Context, cfg Config) (*Executor, error) {
	if cfg.URL == "" {
		return nil, errors.New("remote executor: URL is required")
	}
	if _, err := url.Parse(cfg.URL); err != nil {
		return nil, fmt.Errorf("remote executor: bad URL %q: %w", cfg.URL, err)
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = network.NewSecureHTTPClient()
	}
	if cfg.CancelGrace <= 0 {
		cfg.CancelGrace = 2 * time.Minute
	}
	if cfg.ReconnectMax <= 0 {
		cfg.ReconnectMax = 10 * time.Minute
	}
	e := &Executor{cfg: cfg, client: cfg.HTTPClient}
	var info Info
	if err := e.getJSON(ctx, "/v1/info", &info); err != nil {
		return nil, fmt.Errorf("remote executor: %w", err)
	}
	if info.Protocol != Protocol {
		return nil, fmt.Errorf("remote executor: %s speaks protocol %q, this client speaks %q",
			cfg.URL, info.Protocol, Protocol)
	}
	if info.Name == "" {
		info.Name = "remote"
	}
	if info.MaxConcurrent < 1 {
		info.MaxConcurrent = 1
	}
	e.info = info
	return e, nil
}

// Name reports the build service's name.
func (e *Executor) Name() string { return e.info.Name }

// MaxConcurrent reports how many builds the service runs at once.
func (e *Executor) MaxConcurrent() int { return e.info.MaxConcurrent }

// Phases reports the service's ordered build phases, nil when it declares none.
func (e *Executor) Phases() []service.Phase { return e.info.Phases }

// Run submits the build and follows its event stream to the end.
func (e *Executor) Run(ctx context.Context, h service.ExecutorHandle) (service.BuildStatus, []service.Artifact, error) {
	tmpl, err := h.TemplateYAML()
	if err != nil {
		return "", nil, err
	}
	sub := submission{ID: h.ID(), Template: tmpl, Labels: map[string]string{}}
	for k, v := range e.cfg.Labels {
		sub.Labels[k] = v
	}
	if name := imageName(tmpl); name != "" {
		sub.Labels["image"] = name
	}
	sub.Files = e.referencedFiles(tmpl)

	var state buildState
	if err := e.postJSON(ctx, "/v1/builds", sub, &state); err != nil {
		return "", nil, fmt.Errorf("submitting build: %w", err)
	}
	h.AppendLog(fmt.Sprintf("• build submitted to %s", e.info.Name))
	applyState(h, state)
	if st, ok := terminalStatus(state.Status); ok {
		return st, toArtifacts(state.Artifacts), nil
	}
	return e.follow(ctx, h, 0)
}

// Cancel asks the service to stop the build. A build that has already finished
// is not an error: Run returns its real outcome either way.
func (e *Executor) Cancel(ctx context.Context, h service.ExecutorHandle) error {
	req, err := e.newRequest(ctx, http.MethodPost, "/v1/builds/"+url.PathEscape(h.ID())+"/cancel", nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusAccepted, resp.StatusCode == http.StatusConflict:
		return nil
	default:
		return fmt.Errorf("cancel: %s", readError(resp))
	}
}

// Recover reports what became of a build this client submitted earlier.
func (e *Executor) Recover(ctx context.Context, id string) (service.Recovered, error) {
	var state buildState
	if err := e.getJSON(ctx, "/v1/builds/"+url.PathEscape(id), &state); err != nil {
		return service.Recovered{}, err
	}
	st, ok := terminalStatus(state.Status)
	if !ok {
		return service.Recovered{}, service.ErrStillRunning
	}
	rec := service.Recovered{Status: st, ErrMsg: state.Error, Artifacts: toArtifacts(state.Artifacts), Meta: state.Meta}
	// The log is only on the stream; a finished build replays it and ends.
	stream, err := e.openStream(ctx, id, 0)
	if err != nil {
		return rec, nil
	}
	defer stream.Close()
	_ = readEvents(stream, func(ev event) bool {
		if ev.name == "log" {
			var l logEvent
			if json.Unmarshal(ev.data, &l) == nil {
				rec.Log = append(rec.Log, l.Line)
			}
		}
		return ev.name != "done"
	})
	return rec, nil
}

// follow streams events from index `from` until done, reconnecting on a broken
// stream. A cancelled ctx means the service has been asked to cancel; the
// stream is then followed for CancelGrace longer so the real outcome is
// recorded rather than assumed.
func (e *Executor) follow(
	ctx context.Context, h service.ExecutorHandle, from int64,
) (service.BuildStatus, []service.Artifact, error) {
	streamCtx, stopStream := context.WithCancel(context.Background())
	defer stopStream()
	go e.stopAfterCancelGrace(ctx, streamCtx, stopStream)

	retry := reconnectPolicy{max: e.cfg.ReconnectMax}
	for {
		result, next, opened, err := e.readStream(streamCtx, h, from)
		if result != nil {
			return outcome(*result)
		}
		from = next
		if opened {
			retry.reset() // the service is reachable; only count consecutive failures
		}
		if streamCtx.Err() != nil {
			if ctx.Err() != nil {
				return service.StatusCancelled, nil, nil // cancelled and out of grace: report what we know
			}
			return "", nil, streamCtx.Err()
		}
		// The stream broke without a done event. Ask for the state directly in
		// case the build finished meanwhile, then retry the stream.
		if st, arts, ferr, finished := e.finishedState(streamCtx, h); finished {
			return st, arts, ferr
		}
		if !retry.wait(streamCtx, h.ID(), e.info.Name, err) {
			return "", nil, fmt.Errorf("lost the build's event stream for %s: %v", e.cfg.ReconnectMax, err)
		}
	}
}

// stopAfterCancelGrace ends the stream CancelGrace after ctx is cancelled, so a
// cancelled build is still followed long enough to record its real outcome.
func (e *Executor) stopAfterCancelGrace(ctx, streamCtx context.Context, stop context.CancelFunc) {
	select {
	case <-ctx.Done():
		select {
		case <-time.After(e.cfg.CancelGrace):
			stop()
		case <-streamCtx.Done():
		}
	case <-streamCtx.Done():
	}
}

// readStream opens the event stream at `from` and forwards events to h until
// the stream ends. It returns the done event if one arrived, the index to
// resume from otherwise, and whether the stream opened at all.
func (e *Executor) readStream(
	ctx context.Context, h service.ExecutorHandle, from int64,
) (result *doneEvent, next int64, opened bool, err error) {
	stream, err := e.openStream(ctx, h.ID(), from)
	if err != nil {
		return nil, from, false, err
	}
	defer stream.Close()
	err = readEvents(stream, func(ev event) bool {
		switch ev.name {
		case "log":
			var l logEvent
			if json.Unmarshal(ev.data, &l) == nil {
				h.AppendLog(l.Line)
				from = l.N + 1
			}
		case "phase":
			var p service.Phase
			if json.Unmarshal(ev.data, &p) == nil && p.ID != "" {
				h.SetPhase(p)
			}
		case "meta":
			var m metaEvent
			if json.Unmarshal(ev.data, &m) == nil && m.Key != "" {
				h.PutMeta(m.Key, m.Value)
			}
		case "done":
			var d doneEvent
			if json.Unmarshal(ev.data, &d) == nil {
				result = &d
			}
			return false
		}
		return true
	})
	return result, from, true, err
}

// finishedState asks the service for the build's state and reports it when the
// build has finished meanwhile.
func (e *Executor) finishedState(
	ctx context.Context, h service.ExecutorHandle,
) (service.BuildStatus, []service.Artifact, error, bool) {
	var state buildState
	if err := e.getJSON(ctx, "/v1/builds/"+url.PathEscape(h.ID()), &state); err != nil {
		return "", nil, nil, false
	}
	applyState(h, state)
	st, ok := terminalStatus(state.Status)
	if !ok {
		return "", nil, nil, false
	}
	return st, toArtifacts(state.Artifacts), errorOf(state.Error), true
}

// outcome maps a done event to the service's view of it.
func outcome(d doneEvent) (service.BuildStatus, []service.Artifact, error) {
	st, ok := terminalStatus(d.Status)
	if !ok {
		return service.StatusFailed, toArtifacts(d.Artifacts),
			fmt.Errorf("build service ended the stream with status %q", d.Status)
	}
	return st, toArtifacts(d.Artifacts), errorOf(d.Error)
}

// reconnectPolicy backs off between stream reconnects, 1s doubling to 10s, and
// gives up once the stream has been broken for longer than max.
type reconnectPolicy struct {
	max       time.Duration
	backoff   time.Duration
	firstFail time.Time
}

// reset forgets past failures after a stream opened successfully.
func (r *reconnectPolicy) reset() {
	r.firstFail = time.Time{}
	r.backoff = 0
}

// wait sleeps before the next attempt, or returns false once max has passed.
func (r *reconnectPolicy) wait(ctx context.Context, buildID, service string, cause error) bool {
	if r.firstFail.IsZero() {
		r.firstFail = time.Now()
		r.backoff = time.Second
	} else if time.Since(r.firstFail) > r.max {
		return false
	}
	logger.Logger().Warnf("build %s: event stream from %s interrupted (%v); reconnecting in %s",
		buildID, service, cause, r.backoff)
	select {
	case <-time.After(r.backoff):
	case <-ctx.Done():
	}
	r.backoff = min(r.backoff*2, 10*time.Second)
	return true
}

func applyState(h service.ExecutorHandle, s buildState) {
	for k, v := range s.Meta {
		h.PutMeta(k, v)
	}
	if s.Phase != nil && s.Phase.ID != "" {
		h.SetPhase(*s.Phase)
	}
}

func errorOf(msg string) error {
	if msg == "" {
		return nil
	}
	return errors.New(msg)
}

// referencedFiles collects the auxiliary files a template names relative to the
// templates directory, so the worker can build it without already having them.
// Paths that escape the directory or do not exist are skipped: the worker's own
// copy, if any, then applies.
func (e *Executor) referencedFiles(tmpl string) []file {
	if e.cfg.TemplatesDir == "" {
		return nil
	}
	var t config.ImageTemplate
	if yaml.Unmarshal([]byte(tmpl), &t) != nil {
		return nil
	}
	var rels []string
	for _, af := range t.SystemConfig.AdditionalFiles {
		rels = append(rels, af.Local)
	}
	if t.SystemConfig.Initramfs.Template != "" {
		rels = append(rels, t.SystemConfig.Initramfs.Template)
	}
	root, err := filepath.Abs(e.cfg.TemplatesDir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []file
	for _, rel := range rels {
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "" || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "../") || seen[rel] {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(rel))
		if !strings.HasPrefix(full, root+string(filepath.Separator)) {
			continue
		}
		fi, err := os.Stat(full)
		if err != nil || fi.IsDir() {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		seen[rel] = true
		out = append(out, file{
			Path:          rel,
			ContentBase64: base64.StdEncoding.EncodeToString(data),
			Mode:          fmt.Sprintf("%04o", fi.Mode().Perm()),
		})
	}
	return out
}

// --- HTTP plumbing ---

func (e *Executor) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, e.cfg.URL+path, body)
	if err != nil {
		return nil, err
	}
	if e.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.Token)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (e *Executor) getJSON(ctx context.Context, path string, out any) error {
	req, err := e.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, readError(resp))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Executor) postJSON(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := e.newRequest(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s: %s", path, readError(resp))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Executor) openStream(ctx context.Context, id string, from int64) (io.ReadCloser, error) {
	path := "/v1/builds/" + url.PathEscape(id) + "/events?from=" + strconv.FormatInt(from, 10)
	req, err := e.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("events: %s", readError(resp))
	}
	return resp.Body, nil
}

// readError renders an error response body as "<status>: <message>".
func readError(resp *http.Response) string {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
		return fmt.Sprintf("%s (%s)", e.Error.Message, resp.Status)
	}
	return resp.Status + " " + strings.TrimSpace(string(data))
}
