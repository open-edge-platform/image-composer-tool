package network

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"
)

// FilenameFromURL extracts the filename from an HTTPS URL path.
// Returns an error if the URL is not HTTPS or has no filename component.
func FilenameFromURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	if parsedURL.Scheme != "https" {
		return "", fmt.Errorf("only https URLs are supported, got %q", parsedURL.Scheme)
	}
	baseName := filepath.Base(path.Clean(parsedURL.Path))
	if baseName == "." || baseName == "/" || baseName == "" {
		return "", fmt.Errorf("URL path does not contain a filename")
	}
	return baseName, nil
}

// DownloadFile fetches fileURL and writes it to dstPath.
// When insecureSkipVerify is true, TLS certificate verification is skipped —
// use only for internal/trusted networks with self-signed certificates.
func DownloadFile(fileURL, dstPath string, insecureSkipVerify bool) error {
	var client *http.Client
	if insecureSkipVerify {
		// Clone default transport to retain pooling/timeout defaults, only override TLS.
		base := http.DefaultTransport.(*http.Transport).Clone()
		base.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
		client = &http.Client{
			Timeout:   30 * time.Second,
			Transport: base,
		}
	} else {
		client = NewSecureHTTPClient()
	}

	resp, err := client.Get(fileURL)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	outFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dstPath, err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, resp.Body); err != nil {
		return fmt.Errorf("failed to write destination file %s: %w", dstPath, err)
	}

	return nil
}
