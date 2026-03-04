package pkgfetcher

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/network"
	"github.com/schollz/progressbar/v3"
)

const (
	maxDownloadAttempts = 3
	initialRetryBackoff = 500 * time.Millisecond
)

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

func downloadWithRetry(client *http.Client, url, destPath string, threadcontext int) error {
	log := logger.Logger()

	var lastErr error
	backoff := initialRetryBackoff

	for attempt := 1; attempt <= maxDownloadAttempts; attempt++ {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
		} else {
			func() {
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

				writtenBytes, copyErr := io.Copy(out, resp.Body)
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
		time.Sleep(backoff)
		backoff *= time.Duration(2 * (threadcontext + 1))
	}

	return fmt.Errorf("download failed after %d attempts: %w", maxDownloadAttempts, lastErr)
}

// FetchPackages downloads the given URLs into destDir using a pool of workers.
// It shows a single progress bar tracking files completed vs total.
func FetchPackages(urls []string, destDir string, workers int) error {
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

	// create a shared boolean flag to signal a download error
	var downloadError atomic.Bool

	// start worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for url := range jobs {
				name := path.Base(url)

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
				err := downloadWithRetry(client, url, destPath, i)

				if err != nil {
					log.Errorf("downloading %s failed: %v", url, err)
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

	// error after all jobs done
	if downloadError.Load() {
		return fmt.Errorf("one or more downloads failed")
	}

	if err := bar.Finish(); err != nil {
		log.Errorf("failed to finish progress bar: %v", err)
	}
	return nil
}
