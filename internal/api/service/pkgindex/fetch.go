// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// errNotFound marks a 404, which callers probing several candidate index names
// treat as "try the next one" rather than as a failure.
var errNotFound = errors.New("not found")

// maxIndexBytes caps a single compressed index read. Ubuntu's amd64 main index
// is ~13 MB gzipped, so this leaves generous headroom while stopping a
// misbehaving or hostile mirror from streaming without end.
const maxIndexBytes = 256 << 20

// get issues a GET and returns the response body, which the caller closes.
func get(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %w", url, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch %s: unexpected status %s", url, resp.Status)
	}
	return newLimitedBody(resp.Body), nil
}

// fetchRelease reads releaseDir's Release file and its detached signature,
// for verifyDebRelease to check before trusting an index fetched from the
// same suite.
func fetchRelease(ctx context.Context, client *http.Client, releaseDir string) (release, sig []byte, err error) {
	releaseBody, err := get(ctx, client, releaseDir+"Release")
	if err != nil {
		return nil, nil, fmt.Errorf("fetch Release: %w", err)
	}
	release, err = io.ReadAll(releaseBody)
	releaseBody.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("read Release: %w", err)
	}

	sigBody, err := get(ctx, client, releaseDir+"Release.gpg")
	if err != nil {
		return nil, nil, fmt.Errorf("fetch Release.gpg: %w", err)
	}
	sig, err = io.ReadAll(sigBody)
	sigBody.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("read Release.gpg: %w", err)
	}
	return release, sig, nil
}

// decompress wraps r according to url's suffix. The returned reader must be
// closed; for an uncompressed index that is r itself.
//
// The index is decoded as it streams. The build path writes the decompressed
// copy to disk first, which for Ubuntu main means materialising ~57 MB per
// repository; nothing here needs the bytes twice.
func decompress(url string, r io.ReadCloser) (io.ReadCloser, error) {
	switch {
	case strings.HasSuffix(url, ".gz"):
		zr, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("gzip %s: %w", url, err)
		}
		return chained{Reader: zr, closers: []io.Closer{zr, r}}, nil
	case strings.HasSuffix(url, ".xz"):
		xr, err := xz.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("xz %s: %w", url, err)
		}
		// xz.Reader is not an io.Closer, so only the body needs closing.
		return chained{Reader: xr, closers: []io.Closer{r}}, nil
	case strings.HasSuffix(url, ".zst"):
		// Some dnf repositories publish primary.xml.zst rather than .gz.
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("zstd %s: %w", url, err)
		}
		// zstd.Decoder.Close returns nothing, so it is adapted rather than
		// added to the closer chain directly.
		return chained{Reader: zr, closers: []io.Closer{closeFunc(zr.Close), r}}, nil
	default:
		return r, nil
	}
}

// limitedBody bounds how much of a response body is read while still closing
// the underlying body, which io.LimitReader alone discards.
type limitedBody struct {
	io.Reader
	body io.ReadCloser
}

func newLimitedBody(body io.ReadCloser) io.ReadCloser {
	return limitedBody{Reader: io.LimitReader(body, maxIndexBytes), body: body}
}

func (l limitedBody) Close() error { return l.body.Close() }

// closeFunc adapts a plain release function to io.Closer, for decompressors
// whose Close returns no error.
type closeFunc func()

func (f closeFunc) Close() error {
	f()
	return nil
}

// chained reads from a decompressor while closing it and the body beneath.
type chained struct {
	io.Reader
	closers []io.Closer
}

func (c chained) Close() error {
	var err error
	for _, cl := range c.closers {
		if cerr := cl.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}
