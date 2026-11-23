package debutils

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
)

var errSourceNetwork = errors.New("source repository network error")
var errSourceHTTPStatus = errors.New("source repository HTTP error")
var errSourceMetadataTooLarge = errors.New("source metadata exceeds size limit")

var ErrSourceNetwork = errSourceNetwork
var ErrSourceHTTPStatus = errSourceHTTPStatus
var ErrSourceNotFound = errSourceNotFound

const maxSourceReleaseBytes = 16 << 20
const maxSourceIndexBytes = 128 << 20

type SourceRepositoryOptions struct {
	BaseURL   string
	Suite     string
	Component string
	Key       []byte
	CacheDir  string
}

type SourceRepositoryResult struct {
	Packages      []SourcePackageMetadata
	Release       []byte
	Signature     []byte
	InRelease     bool
	Index         []byte
	IndexMetadata sourceIndexMetadata
	Offline       bool
	Expired       bool
	ValidUntil    time.Time
}

type sourceRepositoryOptions = SourceRepositoryOptions
type sourceRepositoryResult = SourceRepositoryResult

func ReadSourceRepository(ctx context.Context, options SourceRepositoryOptions) (*SourceRepositoryResult, error) {
	return readSourceRepository(ctx, options)
}

func (result *SourceRepositoryResult) FindPackage(name, version string) (*SourcePackageMetadata, error) {
	return findSourcePackage(result.Packages, name, version)
}

func readSourceRepository(ctx context.Context, options sourceRepositoryOptions) (*sourceRepositoryResult, error) {
	result, err := fetchSourceRepository(ctx, options)
	if err != nil {
		if options.CacheDir != "" && errors.Is(err, errSourceNetwork) && ctx.Err() == nil {
			cached, cacheErr := loadSourceCache(options)
			if cacheErr == nil {
				cached.Offline = true
				return cached, nil
			}
			return nil, fmt.Errorf("%w; cached source metadata unavailable: %w", err, cacheErr)
		}
		return nil, err
	}
	if options.CacheDir != "" {
		if err := saveSourceCache(options, result); err != nil {
			return nil, fmt.Errorf("save source metadata cache: %w", err)
		}
	}
	return result, nil
}

func fetchSourceRepository(ctx context.Context, options sourceRepositoryOptions) (*sourceRepositoryResult, error) {
	base, err := sourceRepositoryBaseURL(options.BaseURL)
	if err != nil {
		return nil, err
	}
	if !safeSourcePath(options.Suite, true) || !validSourceComponent(options.Suite) {
		return nil, fmt.Errorf("invalid source repository suite")
	}
	if !validSourceComponent(options.Component) {
		return nil, fmt.Errorf("invalid source repository component")
	}
	if len(options.Key) == 0 {
		return nil, fmt.Errorf("%w: signing key is required", errSourceVerification)
	}
	client := network.NewSecureHTTPClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	result := &sourceRepositoryResult{InRelease: true}
	releasePath := path.Join("dists", options.Suite, "InRelease")
	result.Release, err = fetchSourceMetadata(ctx, client, base, releasePath, maxSourceReleaseBytes)
	if errors.Is(err, errSourceNotFound) {
		result.InRelease = false
		releasePath = path.Join("dists", options.Suite, "Release")
		result.Release, err = fetchSourceMetadata(ctx, client, base, releasePath, maxSourceReleaseBytes)
		if err == nil {
			result.Signature, err = fetchSourceMetadata(ctx, client, base, releasePath+".gpg", maxSourceReleaseBytes)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("fetch source Release metadata: %w", err)
	}
	verified, err := verifySourceRelease(result.Release, result.Signature, options.Key, result.InRelease)
	if err != nil {
		return nil, err
	}
	result.ValidUntil, result.Expired, err = sourceReleaseExpiry(verified)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSourceVerification, err)
	}
	indexes, err := discoverSourceIndexes(verified, options.Component)
	if err != nil {
		if !errors.Is(err, errSourceIndexMissing) {
			return nil, fmt.Errorf("%w: %w", errSourceVerification, err)
		}
		return nil, err
	}
	if options.CacheDir != "" {
		cached, cacheErr := loadSourceCache(options)
		if cacheErr == nil && bytes.Equal(cached.Release, result.Release) &&
			bytes.Equal(cached.Signature, result.Signature) && cached.InRelease == result.InRelease {
			return cached, nil
		}
	}
	for _, entry := range indexes {
		if entry.Size > maxSourceIndexBytes {
			return nil, fmt.Errorf("source index exceeds size limit")
		}
		indexPath := path.Join("dists", options.Suite, entry.Path)
		result.Index, err = fetchSourceMetadata(ctx, client, base, indexPath, int64(entry.Size))
		if errors.Is(err, errSourceNotFound) {
			continue
		}
		if err != nil {
			if errors.Is(err, errSourceMetadataTooLarge) {
				return nil, fmt.Errorf("%w: source index size mismatch", errSourceVerification)
			}
			return nil, fmt.Errorf("fetch source index: %w", err)
		}
		result.IndexMetadata = entry
		break
	}
	if result.IndexMetadata.Path == "" {
		return nil, fmt.Errorf("%w: %w", errSourceIndexMissing, errSourceNotFound)
	}
	if _, err := verifySourceIndex(verified, result.Index, result.IndexMetadata.Path); err != nil {
		return nil, fmt.Errorf("%w: %w", errSourceVerification, err)
	}
	result.Packages, _, err = parseSourceIndexFromRelease(verified, result.Index, result.IndexMetadata.Path)
	if err != nil {
		return nil, err
	}
	for index := range result.Packages {
		artifacts, urlErr := sourceArtifactsWithURLs(options.BaseURL, result.Packages[index])
		if urlErr != nil {
			return nil, fmt.Errorf("resolve source artifact URLs: %w", urlErr)
		}
		result.Packages[index].Artifacts = artifacts
	}
	return result, nil
}

func sourceReleaseExpiry(release []byte) (time.Time, bool, error) {
	fields, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(release))).ReadMIMEHeader()
	if err != nil && err != io.EOF {
		return time.Time{}, false, fmt.Errorf("parse Release expiration: %w", err)
	}
	values := fields.Values("Valid-Until")
	if len(values) == 0 {
		return time.Time{}, false, nil
	}
	if len(values) != 1 {
		return time.Time{}, false, fmt.Errorf("invalid Release Valid-Until")
	}
	validUntil, err := http.ParseTime(values[0])
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid Release Valid-Until: %w", err)
	}
	return validUntil, time.Now().After(validUntil), nil
}

const sourceCacheVersion = 1

type sourceCache struct {
	Version int
	Result  sourceRepositoryResult
}

func sourceCacheIdentity(options sourceRepositoryOptions) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{options.BaseURL, options.Suite, options.Component}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func sourceCacheSnapshotKey(options sourceRepositoryOptions, result *sourceRepositoryResult) string {
	releaseDigest := sha256.Sum256(result.Release)
	identity := fmt.Sprintf("%s\x00%s\x00%x\x00%s\x00%d", sourceCacheIdentity(options), result.IndexMetadata.Path,
		releaseDigest, result.IndexMetadata.SHA256, sourceCacheVersion)
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func saveSourceCache(options sourceRepositoryOptions, result *sourceRepositoryResult) error {
	directory := filepath.Join(filepath.Clean(options.CacheDir), sourceCacheIdentity(options))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create source cache directory: %w", err)
	}
	data, err := json.Marshal(sourceCache{Version: sourceCacheVersion, Result: *result})
	if err != nil {
		return fmt.Errorf("encode source cache: %w", err)
	}
	key := sourceCacheSnapshotKey(options, result)
	if err := writeSourceCacheFile(directory, key+".json", data); err != nil {
		return err
	}
	return writeSourceCacheFile(directory, "latest", []byte(key))
}

func writeSourceCacheFile(directory, name string, data []byte) error {
	file, err := os.CreateTemp(directory, ".source-cache-")
	if err != nil {
		return fmt.Errorf("create staged source cache: %w", err)
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil && !os.IsNotExist(err) {
			logger.Logger().Warnf("failed to remove staged source cache: %v", err)
		}
	}()
	if _, err := file.Write(data); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("write source cache: %w; close: %v", err, closeErr)
		}
		return fmt.Errorf("write source cache: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close source cache: %w", err)
	}
	if err := os.Rename(file.Name(), filepath.Join(directory, name)); err != nil {
		return fmt.Errorf("install source cache: %w", err)
	}
	return nil
}

func loadSourceCache(options sourceRepositoryOptions) (*sourceRepositoryResult, error) {
	directory := filepath.Join(filepath.Clean(options.CacheDir), sourceCacheIdentity(options))
	key, err := os.ReadFile(filepath.Join(directory, "latest"))
	if err != nil {
		return nil, fmt.Errorf("read source cache pointer: %w", err)
	}
	decoded, err := hex.DecodeString(string(key))
	if err != nil || len(decoded) != sha256.Size || string(key) != strings.ToLower(string(key)) {
		return nil, fmt.Errorf("invalid source cache pointer")
	}
	data, err := os.ReadFile(filepath.Join(directory, string(key)+".json"))
	if err != nil {
		return nil, fmt.Errorf("read source cache: %w", err)
	}
	var cache sourceCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.Version != sourceCacheVersion {
		return nil, fmt.Errorf("invalid source cache format")
	}
	result := &cache.Result
	if sourceCacheSnapshotKey(options, result) != string(key) {
		return nil, fmt.Errorf("source cache identity mismatch")
	}
	verified, err := verifySourceRelease(result.Release, result.Signature, options.Key, result.InRelease)
	if err != nil {
		return nil, err
	}
	result.ValidUntil, result.Expired, err = sourceReleaseExpiry(verified)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSourceVerification, err)
	}
	indexes, err := discoverSourceIndexes(verified, options.Component)
	if err != nil {
		if !errors.Is(err, errSourceIndexMissing) {
			return nil, fmt.Errorf("%w: %w", errSourceVerification, err)
		}
		return nil, err
	}
	found := false
	for _, entry := range indexes {
		if entry == result.IndexMetadata {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: cached source index metadata does not match Release", errSourceVerification)
	}
	if _, err := verifySourceIndex(verified, result.Index, result.IndexMetadata.Path); err != nil {
		return nil, fmt.Errorf("%w: cached source index: %w", errSourceVerification, err)
	}
	result.Packages, _, err = parseSourceIndexFromRelease(verified, result.Index, result.IndexMetadata.Path)
	if err != nil {
		return nil, fmt.Errorf("verify cached source index: %w", err)
	}
	for index := range result.Packages {
		result.Packages[index].Artifacts, err = sourceArtifactsWithURLs(options.BaseURL, result.Packages[index])
		if err != nil {
			return nil, fmt.Errorf("resolve cached source artifacts: %w", err)
		}
	}
	return result, nil
}

var errSourceNotFound = errors.New("source repository metadata not found")

func fetchSourceMetadata(ctx context.Context, client *http.Client, base *url.URL, relative string, limit int64) ([]byte, error) {
	endpoint := *base
	endpoint.Path = path.Join(base.Path, relative)
	if !strings.HasPrefix(endpoint.Path, "/") {
		endpoint.Path = "/" + endpoint.Path
	}
	endpoint.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create source metadata request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("%w: %w", errSourceNetwork, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, errSourceNotFound
	}
	if response.StatusCode >= http.StatusInternalServerError {
		return nil, fmt.Errorf("%w: %w: status %d", errSourceNetwork, errSourceHTTPStatus, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", errSourceHTTPStatus, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading metadata: %w", errSourceNetwork, err)
	}
	if int64(len(data)) > limit {
		return nil, errSourceMetadataTooLarge
	}
	return data, nil
}
