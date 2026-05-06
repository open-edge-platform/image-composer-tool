package rpmutils

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/sassoftware/go-rpmutils"
	"github.com/schollz/progressbar/v3"
)

// Result holds the outcome of verifying one RPM.
type Result struct {
	Path     string        // filesystem path to the .rpm
	OK       bool          // signature + checksum OK?
	Duration time.Duration // how long the check took
	Error    error         // any error (signature fail, I/O, etc)
}

// VerifyAll takes a slice of RPM file paths, verifies each one in parallel,
// and returns a slice of results in the same order.
func VerifyAll(paths []string, pubkeyPaths []string, workers int) []Result {
	log := logger.Logger()

	total := len(paths)
	results := make([]Result, total) // allocate up front
	jobs := make(chan int, total)    // channel of indices
	var wg sync.WaitGroup

	// build the progress bar
	bar := progressbar.NewOptions(total,
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(30),
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

	// worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				rpmPath := paths[idx]
				name := filepath.Base(rpmPath)
				bar.Describe("verifying " + name)

				start := time.Now()

				// Try verification with each public key until one succeeds
				var err error
				var allErrors []error
				verified := false

				for _, pubkeyPath := range pubkeyPaths {
					err = verifyWithGoRpm(rpmPath, pubkeyPath)
					if err == nil {
						// Verification succeeded with this key
						verified = true
						break
					}
					allErrors = append(allErrors, fmt.Errorf("key %s: %w", filepath.Base(pubkeyPath), err))
				}

				// Set final error if all keys failed
				if !verified {
					err = fmt.Errorf("verification failed with all %d keys: %v", len(pubkeyPaths), allErrors)
					log.Errorf("verification %s failed with all keys: %v", rpmPath, err)
				}

				results[idx] = Result{
					Path:     rpmPath,
					OK:       verified,
					Duration: time.Since(start),
					Error:    err,
				}

				if err := bar.Add(1); err != nil {
					log.Errorf("failed to add to progress bar: %v", err)
				}
			}
		}()
	}

	// enqueue indices
	for i := range paths {
		jobs <- i
	}
	close(jobs)

	wg.Wait()
	if err := bar.Finish(); err != nil {
		log.Errorf("failed to finish progress bar: %v", err)
	}

	return results
}

// verifyWithGoRpm uses go-rpm to GPG-check + MD5-check a single file.
func verifyWithGoRpm(rpmPath, pubkeyPath string) error {
	// load the keyring
	keyringFile, err := os.Open(pubkeyPath)
	if err != nil {
		return fmt.Errorf("opening public key: %w", err)
	}
	defer keyringFile.Close()

	keyring, err := openpgp.ReadArmoredKeyRing(keyringFile)
	if err != nil {
		return fmt.Errorf("loading keyring: %w", err)
	}

	// open the RPM
	f, err := os.Open(rpmPath)
	if err != nil {
		return fmt.Errorf("opening rpm: %w", err)
	}
	defer f.Close()

	// GPG signature check + MD5 digest check
	_, sigs, err := rpmutils.Verify(f, keyring)
	if err != nil {
		return fmt.Errorf("verify failed: %w", err)
	}

	if len(sigs) == 0 {
		return fmt.Errorf("no GPG signatures found")
	}

	return nil
}
