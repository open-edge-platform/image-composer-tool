package pkgfetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
	"github.com/schollz/progressbar/v3"
)

const (
	maxDownloadAttempts = 3
	initialRetryBackoff = 500 * time.Millisecond
	// bodyIdleTimeout bounds the gap between successful reads of a response
	// body. The shared client's ResponseHeaderTimeout only covers the wait for
	// headers; once the body starts streaming, http has no read deadline, so a
	// proxy that goes silent mid-transfer (headers received, then nothing) would
	// otherwise block io.Copy forever. This does not cap total transfer time —
	// the timer resets on every read that returns bytes — so a slow but
	// progressing large package download is never killed.
	bodyIdleTimeout = 60 * time.Second
)

// idleTimeoutReader wraps an io.ReadCloser and, if no bytes arrive within idle,
// cancels the request context so an in-flight Read is unblocked and a stalled
// mid-body transfer fails (and retries) instead of hanging. The timer resets on
// every read that returns data, so only a true stall — not slow progress —
// trips it.
//
// Cancelling the context (rather than calling rc.Close) is deliberate: a real
// net/http HTTP/1.x response body serializes Close behind the in-flight Read,
// so a Close-based watchdog would itself block on the stalled read and leave
// the build hung. Cancelling the request makes http.Transport tear down the
// underlying connection, which is the mechanism that reliably unblocks the read.
type idleTimeoutReader struct {
	rc      io.ReadCloser
	idle    time.Duration
	cancel  context.CancelFunc
	timer   *time.Timer
	tripped atomic.Bool
}

func newIdleTimeoutReader(rc io.ReadCloser, idle time.Duration, cancel context.CancelFunc) *idleTimeoutReader {
	r := &idleTimeoutReader{rc: rc, idle: idle, cancel: cancel}
	r.timer = time.AfterFunc(idle, func() {
		r.tripped.Store(true)
		r.cancel()
	})
	return r
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n > 0 {
		r.timer.Reset(r.idle)
	}
	if err != nil && r.tripped.Load() {
		return n, fmt.Errorf("stalled: no data received for %s: %w", r.idle, err)
	}
	return n, err
}

// stop halts the watchdog. Call it once the body has been fully read so the
// timer does not linger; it does not close the underlying reader, which the
// caller's deferred resp.Body.Close handles.
func (r *idleTimeoutReader) stop() {
	r.timer.Stop()
}

func shouldRetryHTTPStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func downloadWithRetry(ctx context.Context, client *http.Client, url, destPath string, threadcontext int) error {
	log := logger.Logger()

	var lastErr error
	backoff := initialRetryBackoff

	for attempt := 1; attempt <= maxDownloadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("download cancelled before attempt %d: %w", attempt, err)
		}
		// Per-attempt cancellable context so the idle watchdog can tear down a
		// stalled exchange without affecting the caller's ctx or later attempts.
		reqCtx, cancel := context.WithCancel(ctx)
		req, reqErr := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if reqErr != nil {
			cancel()
			return fmt.Errorf("build request: %w", reqErr)
		}
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Pragma", "no-cache")
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
		} else {
			func() {
				defer cancel()
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					if shouldRetryHTTPStatus(resp.StatusCode) {
						lastErr = fmt.Errorf("transient status: %s", resp.Status)
						return
					}
					lastErr = fmt.Errorf("bad status: %s", resp.Status)
					return
				}

				out, createErr := os.Create(destPath)
				if createErr != nil {
					lastErr = createErr
					return
				}
				defer out.Close()

				body := newIdleTimeoutReader(resp.Body, bodyIdleTimeout, cancel)
				defer body.stop()
				writtenBytes, copyErr := io.Copy(out, body)
				if copyErr != nil {
					lastErr = copyErr
					if removeErr := os.Remove(destPath); removeErr != nil && !os.IsNotExist(removeErr) {
						log.Warnf("failed to remove partial file %s: %v", destPath, removeErr)
					}
					return
				}

				if writtenBytes == 0 || (resp.ContentLength >= 0 && writtenBytes != resp.ContentLength) {
					expectedBytes := "unknown"
					if resp.ContentLength >= 0 {
						expectedBytes = fmt.Sprintf("%d", resp.ContentLength)
					}

					lastErr = fmt.Errorf("incomplete response body: got %d bytes, expected %s", writtenBytes, expectedBytes)
					log.Warnf("response body validation failed for %s: got %d bytes, expected %s; removing %s", url, writtenBytes, expectedBytes, destPath)
					if removeErr := os.Remove(destPath); removeErr != nil && !os.IsNotExist(removeErr) {
						log.Warnf("failed to remove incomplete file %s: %v", destPath, removeErr)
					}
					return
				}

				lastErr = nil
			}()

			if lastErr == nil {
				return nil
			}

			if resp.StatusCode != http.StatusOK && !shouldRetryHTTPStatus(resp.StatusCode) {
				return lastErr
			}
		}

		if attempt == maxDownloadAttempts {
			break
		}

		log.Warnf("download attempt %d/%d failed for %s: %v; retrying in %s", attempt, maxDownloadAttempts, url, lastErr, backoff)
		// Cancel-aware sleep: break out immediately on ctx cancel so a SIGINT
		// during download does not have to wait for the retry backoff to
		// elapse before the worker exits.
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("download cancelled during retry backoff: %w", ctx.Err())
		case <-timer.C:
		}
		backoff *= time.Duration(2 * (threadcontext + 1))
	}

	return fmt.Errorf("download failed after %d attempts: %w", maxDownloadAttempts, lastErr)
}

// maxReportedFailures bounds the failure list in the returned error. A broken
// proxy fails every package, and an error carrying hundreds of near-identical
// lines is as unreadable as one carrying none; the remainder stays in the log,
// where each failure was already logged individually.
const maxReportedFailures = 5

// summarizeFailures renders collected failures as indented lines appended to the
// error message, truncating past maxReportedFailures and saying how many were
// omitted (a silent cut would misrepresent the scale of the problem).
func summarizeFailures(failed []string) string {
	if len(failed) == 0 {
		return "" // downloadError set without a recorded URL; nothing to add
	}
	shown := failed
	if len(shown) > maxReportedFailures {
		shown = shown[:maxReportedFailures]
	}
	var b strings.Builder
	for _, f := range shown {
		b.WriteString("\n  - ")
		b.WriteString(f)
	}
	if omitted := len(failed) - len(shown); omitted > 0 {
		fmt.Fprintf(&b, "\n  ... and %d more (see the log for every failure)", omitted)
	}
	return b.String()
}

// FetchPackages downloads the given URLs into destDir using a pool of workers.
// It shows a single progress bar tracking files completed vs total. The ctx
// is threaded through to every HTTP request and retry-backoff sleep so a
// SIGINT/SIGTERM during download aborts in-flight HTTP work within one
// retry-backoff quantum. After cancellation the workers still drain the
// remaining queued URLs (each drains near-instantly since it skips HTTP work
// and only advances the progress bar), keeping bar.Add balanced with the
// initial job count so bar.Finish reports a coherent state.
func FetchPackages(ctx context.Context, urls []string, destDir string, workers int) error {
	log := logger.Logger()

	total := len(urls)
	jobs := make(chan string, total)
	var wg sync.WaitGroup

	// create a single progress bar for total files
	bar := progressbar.NewOptions(total,
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWidth(30),
		progressbar.OptionShowCount(),
		progressbar.OptionThrottle(200*time.Millisecond),
		progressbar.OptionSpinnerType(10),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	// Collect the URLs that failed, not just the fact that something did. With a
	// bare boolean the caller can only report "one or more downloads failed", which
	// for a large image means the actionable detail (which package, and why) is
	// buried thousands of progress-bar lines deep in the log. The distinction is
	// worth naming: a 404 on a single package usually means stale cached metadata
	// pointing at a version the mirror has since superseded, whereas a connection
	// error across many packages means the network or proxy is wrong. Those need
	// different fixes, so the error text has to say which one happened.
	var (
		failedMu sync.Mutex
		failed   []string
	)
	recordFailure := func(url string, err error) {
		failedMu.Lock()
		defer failedMu.Unlock()
		failed = append(failed, fmt.Sprintf("%s: %v", url, err))
	}
	// create a shared boolean flag to signal a download error
	var downloadError atomic.Bool

	// start worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for url := range jobs {
				// Drain remaining jobs quickly when the ambient ctx is
				// cancelled: skip the HTTP work but still bar.Add so wg.Wait
				// doesn't hang on an under-incremented progress bar. Setting
				// downloadError ensures FetchPackages returns non-nil.
				if err := ctx.Err(); err != nil {
					downloadError.Store(true)
					if err := bar.Add(1); err != nil {
						log.Errorf("failed to add to progress bar: %v", err)
					}
					continue
				}
				parsedURL, parseErr := urlpkg.Parse(url)
				if parseErr != nil || parsedURL.Path == "" {
					parsedURL = &urlpkg.URL{Path: strings.SplitN(url, "?", 2)[0]}
				}
				name := path.Base(parsedURL.Path)

				// update description to current file
				bar.Describe(name)

				// ensure destination directory exists
				if err := os.MkdirAll(destDir, 0755); err != nil {
					log.Errorf("failed to create dest dir %s: %v", destDir, err)
					if err := bar.Add(1); err != nil {
						log.Errorf("failed to add to progress bar: %v", err)
					}
					continue
				}

				destPath := filepath.Join(destDir, name)
				if fi, err := os.Stat(destPath); err == nil {
					if fi.Size() > 0 {
						//log.Debugf("skipping existing %s", name)
						if err := bar.Add(1); err != nil {
							log.Errorf("failed to add to progress bar: %v", err)
						}
						continue
					}
					// file exists but zero size: re-download
					log.Warnf("re-downloading zero-size %s", name)
				}
				client := network.GetSecureHTTPClient()
				// S3/CloudFront treats literal '+' as space; encode it as %2B in the
				// download URL only (the local filename keeps the original '+').
				downloadURL := strings.ReplaceAll(url, "+", "%2B")
				err := downloadWithRetry(ctx, client, downloadURL, destPath, i)

				if err != nil {
					log.Errorf("downloading %s failed: %v", url, err)
					recordFailure(url, err)
					downloadError.Store(true)
				}
				// increment progress bar
				if err := bar.Add(1); err != nil {
					log.Errorf("failed to add to progress bar: %v", err)
				}
			}
		}()
	}

	// enqueue jobs
	for _, u := range urls {
		jobs <- u
	}
	close(jobs)

	wg.Wait()

	// error after all jobs done — prefer surfacing ctx cancellation so
	// callers can distinguish "user aborted" from a real download failure.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("package download cancelled: %w", err)
	}
	if downloadError.Load() {
		return fmt.Errorf("%d of %d package downloads failed:%s", len(failed), total, summarizeFailures(failed))
	}

	if err := bar.Finish(); err != nil {
		log.Errorf("failed to finish progress bar: %v", err)
	}
	fmt.Println()
	return nil
}
