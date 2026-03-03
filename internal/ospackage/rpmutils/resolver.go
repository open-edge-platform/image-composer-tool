package rpmutils

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/network"
)

const (
	metadataMaxDownloadAttempts = 3
	metadataRetryBackoff        = 500 * time.Millisecond
)

func shouldRetryMetadataStatus(statusCode int) bool {
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

func fetchURLWithRetry(client *http.Client, targetURL, resourceName string) ([]byte, error) {
	log := logger.Logger()

	backoff := metadataRetryBackoff
	var lastErr error

	for attempt := 1; attempt <= metadataMaxDownloadAttempts; attempt++ {
		resp, err := client.Get(targetURL)
		if err != nil {
			lastErr = err
		} else {
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				if shouldRetryMetadataStatus(resp.StatusCode) {
					lastErr = fmt.Errorf("transient status: %s", resp.Status)
				} else {
					return nil, fmt.Errorf("GET %s: bad status: %s", targetURL, resp.Status)
				}
			} else {
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					lastErr = readErr
				} else {
					return body, nil
				}
			}
		}

		if attempt == metadataMaxDownloadAttempts {
			break
		}

		log.Warnf("attempt %d/%d downloading %s failed: %v; retrying in %s", attempt, metadataMaxDownloadAttempts, resourceName, lastErr, backoff)
		time.Sleep(backoff)
		backoff *= 2
	}

	return nil, fmt.Errorf("GET %s failed after %d attempts: %w", targetURL, metadataMaxDownloadAttempts, lastErr)
}

// extractBaseRequirement takes a potentially complex requirement string
// and returns only the base package/capability name.
// Examples:
//   - "libc.so.6(GLIBC_2.38)(64bit)" -> "libc.so.6"
//   - "libsemanage.so.2(LIBSEMANAGE_1.0)(64bit)" -> "libsemanage.so.2"
//   - "(coreutils or busybox)" -> "coreutils"
//   - "filesystem >= 3.0" -> "filesystem"
func extractBaseRequirement(req string) string {
	req = strings.TrimSpace(req)
	if req == "" {
		return ""
	}

	// Handle complex conditional dependencies with "if" clauses
	if strings.Contains(req, ") if ") {
		// Extract content between first '((' and ') if'
		if start := strings.Index(req, "(("); start != -1 {
			if end := strings.Index(req, ") if "); end != -1 {
				inner := req[start+2 : end]
				// Handle multiple operators in priority order
				for _, op := range []string{" >= ", " <= ", " > ", " < ", " = "} {
					if idx := strings.Index(inner, op); idx != -1 {
						return strings.TrimSpace(inner[:idx])
					}
				}
				return strings.TrimSpace(inner)
			}
		}
	}

	// Handle simple parentheses cases
	if strings.HasPrefix(req, "(") && strings.HasSuffix(req, ")") {
		inner := req[1 : len(req)-1]
		inner = strings.TrimSpace(inner)
		// Handle version operators in priority order
		for _, op := range []string{" >= ", " <= ", " > ", " < ", " = "} {
			if idx := strings.Index(inner, op); idx != -1 {
				return strings.TrimSpace(inner[:idx])
			}
		}
		parts := strings.Fields(inner)
		if len(parts) > 0 {
			return parts[0]
		}
		return inner
	}

	// Handle regular cases with operators
	for _, op := range []string{" >= ", " <= ", " > ", " < ", " = "} {
		if idx := strings.Index(req, op); idx != -1 {
			return strings.TrimSpace(req[:idx])
		}
	}

	finalParts := strings.Fields(req)
	if len(finalParts) == 0 {
		return ""
	}
	base := finalParts[0]

	// Remove all parenthesized suffixes like (GLIBC_2.38)(64bit), (LIBSEMANAGE_1.0)(64bit), etc.
	// Keep removing until no more parentheses at the end
	for {
		idx := strings.Index(base, "(")
		if idx == -1 {
			break
		}
		base = base[:idx]
	}

	return base
}

func GenerateDot(pkgs []ospackage.PackageInfo, file string, pkgSources map[string]config.PackageSource) error {
	log := logger.Logger()
	log.Infof("Generating DOT file %s", file)

	outFile, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("creating DOT file: %w", err)
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	if _, err := fmt.Fprintln(writer, "digraph G {"); err != nil {
		return fmt.Errorf("writing DOT header: %w", err)
	}
	if _, err := fmt.Fprintln(writer, "  rankdir=LR;"); err != nil {
		return fmt.Errorf("writing DOT attributes: %w", err)
	}
	if _, err := fmt.Fprintln(writer, "  node [shape=box];"); err != nil {
		return fmt.Errorf("writing DOT node defaults: %w", err)
	}

	edgesWritten := make(map[string]bool)

	for _, pkg := range pkgs {
		if pkg.Name == "" {
			continue
		}
		// Extract clean package name for display (e.g., "libgcrypt" instead of "libgcrypt-1.10.3-1.azl3.x86_64.rpm")
		// Note: Multiple package versions (e.g., glibc-2.38 and glibc-2.35) will both produce "glibc"
		// This causes duplicate node declarations in the DOT file, which is valid - GraphViz merges them.
		// For visualization purposes, we only care about package relationships, not specific versions.
		cleanName := extractBasePackageNameFromFile(pkg.Name)
		if _, err := fmt.Fprintf(writer, "  \"%s\";\n", cleanName); err != nil {
			return fmt.Errorf("writing DOT node for %s: %w", cleanName, err)
		}
		for _, dep := range pkg.Requires {
			if dep == "" {
				continue
			}
			// Extract clean dependency name for edges (handles capabilities and package requirements)
			cleanDep := extractBaseRequirement(dep)
			// Also trim package filenames (e.g., "glibc-2.38-16.azl3.x86_64.rpm" -> "glibc")
			cleanDep = extractBasePackageNameFromFile(cleanDep)
			edgeKey := cleanName + "|" + cleanDep
			if edgesWritten[edgeKey] {
				continue
			}
			if _, err := fmt.Fprintf(writer, "  \"%s\" -> \"%s\";\n", cleanName, cleanDep); err != nil {
				return fmt.Errorf("writing DOT edge %s->%s: %w", cleanName, cleanDep, err)
			}
			edgesWritten[edgeKey] = true
		}
	}

	if _, err := fmt.Fprintln(writer, "}"); err != nil {
		return fmt.Errorf("writing DOT footer: %w", err)
	}

	return nil
}

// matchesPackageFilter checks if a package name matches any of the filter patterns.
// Supports glob patterns, exact match, and version-specific prefix match
// (e.g., "kernel-6.17.11" matches "kernel-6.17.11-1.emt3.x86_64.rpm").
func matchesPackageFilter(pkgName string, filter []string) bool {
	if len(filter) == 0 {
		return true // No filter means include all
	}

	for _, pattern := range filter {
		if isGlobPattern(pattern) {
			if ok, err := path.Match(pattern, pkgName); err == nil && ok {
				return true
			}
		}

		// Exact match
		if pkgName == pattern {
			return true
		}
		// Prefix match with version (e.g., "kernel-drivers-gpu-6.17.11" matches "kernel-drivers-gpu-6.17.11-1.emt3.x86_64")
		if strings.HasPrefix(pkgName, pattern+"-") || strings.HasPrefix(pkgName, pattern) {
			return true
		}
	}
	return false
}

// ParseRepositoryMetadata parses the repodata/primary.xml(.gz/.zst) file from a given base URL.
// If packageFilter is non-empty, only packages matching the filter (by name prefix) will be included.
// It also caches the downloaded and uncompressed XML files for debugging purposes.
func ParseRepositoryMetadata(baseURL, gzHref string, packageFilter []string) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	fullURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(gzHref, "/")
	log.Infof("Fetching and parsing repository metadata from %s", fullURL)

	// Create cache directory for XML files using same pattern as debutils
	globalCache := config.TempDir()
	metadataDirName := generateRPMMetadataDir(baseURL)
	xmlCacheDir := filepath.Join(globalCache, "builds", metadataDirName)
	if err := os.MkdirAll(xmlCacheDir, 0755); err != nil {
		log.Warnf("Failed to create XML cache directory: %v", err)
		xmlCacheDir = "" // Disable caching if directory creation fails
	}

	client := network.NewSecureHTTPClient()
	// First, fetch compressed XML with retry on transient failures
	compressedData, err := fetchURLWithRetry(client, fullURL, "repository metadata")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch compressed metadata: %w", err)
	}

	// Save the original compressed file
	if xmlCacheDir != "" {
		saveOriginalXML(xmlCacheDir, gzHref, baseURL, compressedData)
	}

	var gr io.ReadCloser
	ext := strings.ToLower(filepath.Ext(gzHref))
	reader := bytes.NewReader(compressedData)
	switch ext {
	case ".gz":
		gr, err = gzip.NewReader(reader)

	case ".zst":
		zstDecoder, err := zstd.NewReader(reader)
		if err != nil {
			return nil, err
		}
		gr = zstDecoder.IOReadCloser()

	default:
		err = fmt.Errorf("unsupported compression type %s", ext)
	}

	if err != nil {
		return nil, err
	}
	defer gr.Close()

	// Read and save the uncompressed XML
	var xmlBuffer bytes.Buffer
	teeReader := io.TeeReader(gr, &xmlBuffer)

	dec := xml.NewDecoder(teeReader)

	var (
		infos   []ospackage.PackageInfo
		curInfo *ospackage.PackageInfo
	)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		switch elem := tok.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "package":
				// start a new PackageInfo
				curInfo = &ospackage.PackageInfo{}
				curInfo.Type = "rpm"

			case "version":
				// Parse version attributes and combine them
				var epoch, ver, rel string
				for _, attr := range elem.Attr {
					switch attr.Name.Local {
					case "epoch":
						epoch = attr.Value
					case "ver":
						ver = attr.Value
					case "rel":
						rel = attr.Value
					}
				}

				// Build version string in format: epoch:ver-rel
				if curInfo != nil {
					// Fill missing fields with "0"
					if epoch == "" {
						epoch = "0"
					}
					if ver == "" {
						ver = "0"
					}
					if rel == "" {
						rel = "0"
					}
					versionStr := fmt.Sprintf("%s:%s-%s", epoch, ver, rel)
					curInfo.Version = versionStr
				}

			case "location":
				// read the href and build full URL + infer Name (filename)
				for _, a := range elem.Attr {
					if a.Name.Local == "href" {
						curInfo.URL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(a.Value, "/")
						curInfo.Name = path.Base(a.Value)
						break
					}
				}

			case "format":
				const rpmNS = "http://linux.duke.edu/metadata/rpm"

				// parse everything inside <format> (including rpm:provides/requires)
				section := "" // "provides" | "requires" | ""

			FormatLoop:
				for {
					tok2, err2 := dec.Token()
					if err2 != nil {
						break // EOF or error
					}
					switch inner := tok2.(type) {
					case xml.StartElement:
						switch {
						case inner.Name.Local == "license" && inner.Name.Space == rpmNS:
							if tok3, err := dec.Token(); err == nil {
								if cd, ok := tok3.(xml.CharData); ok && curInfo != nil {
									curInfo.License = strings.TrimSpace(string(cd))
								}
							}

						case inner.Name.Local == "vendor" && inner.Name.Space == rpmNS:
							if tok3, err := dec.Token(); err == nil {
								if cd, ok := tok3.(xml.CharData); ok && curInfo != nil {
									curInfo.Origin = strings.TrimSpace(string(cd))
								}
							}

						case inner.Name.Local == "provides" && inner.Name.Space == rpmNS:
							section = "provides"

						case inner.Name.Local == "requires" && inner.Name.Space == rpmNS:
							section = "requires"

						case inner.Name.Local == "entry" && inner.Name.Space == rpmNS:
							// rpm:entry name="..." ver="..." rel="..." epoch="..." flags="..."
							var name, version, release, epoch, flags string
							for _, a := range inner.Attr {
								switch a.Name.Local {
								case "name":
									name = a.Value
								case "ver":
									version = a.Value
								case "rel":
									release = a.Value
								case "epoch":
									epoch = a.Value
								case "flags":
									flags = a.Value
								}
							}
							if name != "" && curInfo != nil {
								if section == "provides" {
									curInfo.Provides = append(curInfo.Provides, name)
								} else if section == "requires" {
									// Store the base name in Requires
									curInfo.Requires = append(curInfo.Requires, name)

									// Store version constraint with package name prefix in RequiresVer
									if version != "" || release != "" || epoch != "" || flags != "" {
										versionPart := ""
										if epoch != "" {
											versionPart = epoch + ":"
										}
										if version != "" {
											versionPart += version
										}
										if release != "" {
											versionPart += "-" + release
										}

										var versionConstraint string
										if flags != "" && versionPart != "" {
											// Convert flags to readable format (GE = >=, EQ = =, etc.)
											operator := convertFlags(flags)
											versionConstraint = fmt.Sprintf("%s (%s %s)", name, operator, versionPart) // samuel (>=2.3)
										} else if versionPart != "" {
											// Version info but no operator, assume equality
											versionConstraint = fmt.Sprintf("%s = %s", name, versionPart)
										} else {
											// Only package name
											versionConstraint = name
										}
										curInfo.RequiresVer = append(curInfo.RequiresVer, versionConstraint)
									} else {
										// No version constraint, just store the package name
										curInfo.RequiresVer = append(curInfo.RequiresVer, name)
									}
								}
							}

						// some repos list <file> entries inside <format> without a namespace
						case inner.Name.Local == "file" && inner.Name.Space != rpmNS:
							if tok3, err := dec.Token(); err == nil {
								if cd, ok := tok3.(xml.CharData); ok && curInfo != nil {
									curInfo.Files = append(curInfo.Files, strings.TrimSpace(string(cd)))
								}
							}
						}

					case xml.EndElement:
						switch {
						case inner.Name.Local == "provides" && inner.Name.Space == rpmNS:
							section = ""
						case inner.Name.Local == "requires" && inner.Name.Space == rpmNS:
							section = ""
						case inner.Name.Local == "format":
							break FormatLoop
						}
					}
				}

			case "name":
				// canonical package name
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						curInfo.Name = string(cd)
						curInfo.PkgName = string(cd) // store canonical package name in PkgName field
					}
				}

			case "description":
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						curInfo.Description = string(cd)
					}
				}

			case "arch":
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						curInfo.Arch = string(cd)
					}
				}

			case "checksum":
				// primary.xml checksum for the rpm payload (outside <format>)
				cs := ospackage.Checksum{}
				for _, attr := range elem.Attr {
					if attr.Name.Local == "type" {
						cs.Algorithm = strings.ToUpper(attr.Value) // SHA256, etc.
						break
					}
				}
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						cs.Value = string(cd)
						curInfo.Checksums = append(curInfo.Checksums, cs)
					}
				}

			case "file":
				// sometimes <file> is outside <format> as well
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						curInfo.Files = append(curInfo.Files, strings.TrimSpace(string(cd)))
					}
				}
			}

		case xml.EndElement:
			switch elem.Name.Local {
			case "package":
				if curInfo.Arch == "src" {
					continue
				}
				// Apply package filter if specified
				if len(packageFilter) > 0 && !matchesPackageFilter(curInfo.Name, packageFilter) {
					continue
				}
				// finish this package
				infos = append(infos, *curInfo)
			}
		}
	}

	// Save the uncompressed XML file
	if xmlCacheDir != "" {
		saveUncompressedXML(xmlCacheDir, gzHref, baseURL, xmlBuffer.Bytes())
	}

	return infos, nil
}

// FetchPrimaryURL downloads repomd.xml and returns the href of the primary metadata.
// It also saves the repomd.xml file to cache for debugging purposes.
func FetchPrimaryURL(repomdURL string) (string, error) {
	log := logger.Logger()

	client := network.NewSecureHTTPClient()
	repomdData, err := fetchURLWithRetry(client, repomdURL, "repomd.xml")
	if err != nil {
		return "", err
	}

	// Save repomd.xml file using same pattern as debutils
	globalCache := config.TempDir()
	baseURL := strings.TrimSuffix(repomdURL, "/repodata/repomd.xml")
	metadataDirName := generateRPMMetadataDir(baseURL)
	xmlCacheDir := filepath.Join(globalCache, "builds", metadataDirName)
	if err := os.MkdirAll(xmlCacheDir, 0755); err == nil {
		urlHash := sha256.Sum256([]byte(baseURL))
		urlHashStr := hex.EncodeToString(urlHash[:])[:8]
		timestamp := time.Now().Format("2006-01-02_15-04-05")
		filename := fmt.Sprintf("repomd_%s_%s.xml", urlHashStr, timestamp)
		filePath := filepath.Join(xmlCacheDir, filename)
		if writeErr := os.WriteFile(filePath, repomdData, 0644); writeErr == nil {
			log.Infof("Saved repomd.xml file: %s", filePath)
		} else {
			log.Warnf("Failed to save repomd.xml file: %v", writeErr)
		}
	}

	dec := xml.NewDecoder(bytes.NewReader(repomdData))

	// Walk the tokens looking for <data type="primary">
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "data" {
			continue
		}
		// Check its type attribute
		var isPrimary bool
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && attr.Value == "primary" {
				isPrimary = true
				break
			}
		}
		if !isPrimary {
			// Skip this <data> section
			if err := dec.Skip(); err != nil {
				return "", fmt.Errorf("error skipping token: %w", err)
			}
			continue
		}

		// Inside <data type="primary">, look for <location href="..."/>
		for {
			tok2, err := dec.Token()
			if err != nil {
				if err == io.EOF {
					break
				}
				return "", err
			}
			// If we hit the end of this <data> element, bail out
			if ee, ok := tok2.(xml.EndElement); ok && ee.Name.Local == "data" {
				break
			}
			if le, ok := tok2.(xml.StartElement); ok && le.Name.Local == "location" {
				// Pull the href attribute
				for _, attr := range le.Attr {
					if attr.Name.Local == "href" {
						return attr.Value, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("primary location not found in %s", repomdURL)
}

func GetRepoMetaDataURL(baseURL, repoMetaXmlPath string) string {
	repoMetaDataURL := strings.TrimRight(baseURL, "/") + "/" + repoMetaXmlPath
	// Check if baseURL is a valid URL,
	if !strings.HasPrefix(repoMetaDataURL, "http://") && !strings.HasPrefix(repoMetaDataURL, "https://") {
		return ""
	}
	return repoMetaDataURL
}

// Helper function to convert RPM flags to readable operators
func convertFlags(flags string) string {
	switch flags {
	case "EQ":
		return "="
	case "GE":
		return ">="
	case "LE":
		return "<="
	case "GT":
		return ">"
	case "LT":
		return "<"
	default:
		return flags
	}
}

// MatchRequested matches requested package names to the best available versions in the repo.
func MatchRequested(requests []string, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {

	var out []ospackage.PackageInfo
	seen := make(map[string]struct{})
	for _, want := range requests {
		if isGlobPattern(want) {
			pkgs, found := ResolveWildcardPackageConflicts(want, all)
			if !found {
				return nil, fmt.Errorf("requested package '%q' not found in repo", want)
			}
			for _, pkg := range pkgs {
				key := fmt.Sprintf("%s=%s", pkg.Name, pkg.Version)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, pkg)
			}
			continue
		}

		if pkg, found := ResolveTopPackageConflicts(want, all); found {
			key := fmt.Sprintf("%s=%s", pkg.Name, pkg.Version)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, pkg)
		} else {
			return nil, fmt.Errorf("requested package '%q' not found in repo", want)
		}
	}
	return out, nil
}

// ResolveDependencies takes a seed list of PackageInfos (the exact versions
// matched) and the full list of all PackageInfos from the repo, and
// returns the minimal closure of PackageInfos needed to satisfy all Requires.
func ResolveDependencies(requested []ospackage.PackageInfo, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	// Build maps for fast lookup
	byNameVer := make(map[string]ospackage.PackageInfo, len(all))
	for _, pi := range all {
		if pi.Version != "" {
			key := fmt.Sprintf("%s=%s", pi.Name, pi.Version)
			byNameVer[key] = pi
		}
	}

	// Clear required fields for all and requested
	for i := range all {
		all[i].Requires = nil
	}
	for i := range requested {
		requested[i].Requires = nil
	}

	neededSet := make(map[string]struct{})
	queue := make([]ospackage.PackageInfo, 0, len(requested))

	// Initialize queue with requested packages
	for _, pi := range requested {
		if pi.Version != "" {
			key := fmt.Sprintf("%s=%s", pi.Name, pi.Version)
			if pkg, ok := byNameVer[key]; ok {
				queue = append(queue, pkg)
				continue
			}
		}
		return nil, fmt.Errorf("requested package %q not in repo listing", pi.Name)
	}

	// Use a map to store results so we can modify them
	resultMap := make(map[string]*ospackage.PackageInfo)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if _, seen := neededSet[cur.Name]; seen {
			continue
		}
		neededSet[cur.Name] = struct{}{}

		// Store a copy in the result map so we can modify it
		curCopy := cur
		resultMap[cur.Name] = &curCopy

		// Process dependencies
		for _, dep := range cur.RequiresVer {
			// Use proper dependency name cleaning
			// depName := extractBaseRequirement(dep)
			depName := extractBaseNameFromDep(dep)
			filename, seen := findMatchingKeyInNeededSet(neededSet, depName)
			if depName == "" || neededSet[filename] != struct{}{} {
				continue
			}

			// Check if already resolved
			// if _, seen := neededSet[depName]; seen {
			if seen {
				// ENHANCEMENT: Check version compatibility for already-resolved dependencies
				existing, err := findAllCandidates(cur, depName, queue) //convertMapToSlice(resultMap))
				if err == nil && len(existing) > 0 {
					// Validate that existing package satisfies current requirement
					_, err := resolveMultiCandidates(cur, existing)
					if err != nil {
						// Find the specific version constraint from RequiresVer
						var requiredVer string
						for _, req := range cur.RequiresVer {
							if strings.Contains(req, depName) {
								requiredVer = req
								break
							}
						}
						return nil, fmt.Errorf("conflicting package dependencies: %s_%s requires %s, but %s is already selected",
							cur.Name, cur.Version, requiredVer, existing[0].Name)
					}
				}
				// Append to parent's Requires field even if already resolved
				if resultPkg, exists := resultMap[cur.Name]; exists {
					resultPkg.Requires = append(resultPkg.Requires, filename)
				}

				continue
			}

			// Find candidates for this dependency
			candidates, err := findAllCandidates(cur, depName, all)
			if err != nil {
				return nil, fmt.Errorf("failed to find candidates for dependency %q of package %q: %v", depName, cur.Name, err)
			}

			if len(candidates) >= 1 {
				chosenCandidate, err := resolveMultiCandidates(cur, candidates)
				if err != nil {
					log.Errorf("failed to resolve multiple candidates for dependency %q of package %q: %v", depName, cur.Name, err)
					return nil, fmt.Errorf("failed to resolve multiple candidates for dependency %q of package %q: %v", depName, cur.Name, err)
				}

				// Update the parent's Requires field with the chosen candidate's name
				if resultPkg, exists := resultMap[cur.Name]; exists {
					resultPkg.Requires = append(resultPkg.Requires, chosenCandidate.Name)
				}

				// Add chosen candidate to the queue for further processing
				queue = append(queue, chosenCandidate)
			} else {
				// FAIL FAST instead of just warning
				// return nil, fmt.Errorf("no candidates found for required dependency %q of package %q", depName, cur.Name)
				log.Warnf("No candidates found for required dependency %q of package %q", depName, cur.Name)
			}
		}
	}

	// Convert result map back to slice
	result := make([]ospackage.PackageInfo, 0, len(resultMap))
	for _, pkg := range resultMap {
		result = append(result, *pkg)
	}

	// Sort result by package name for determinism
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	log.Infof("Successfully resolved %d packages from %d requested packages", len(result), len(requested))
	return result, nil
}

// findMatchingKeyInNeededSet checks if any key in neededSet contains depName as a substring,
// and returns the first matching key whose base package name equals depName.
func findMatchingKeyInNeededSet(neededSet map[string]struct{}, depName string) (string, bool) {
	for k := range neededSet {
		if strings.Contains(k, depName) {
			fileName := extractBasePackageNameFromFile(k)
			if fileName == depName {
				return k, true
			}
		}
	}
	return "", false
}

// generateRPMMetadataDir creates a dynamic directory name for RPM metadata storage
// following the same pattern as debutils: <repoId>_<arch>_<type>
func generateRPMMetadataDir(baseURL string) string {
	// Extract meaningful identifier from URL
	urlHash := sha256.Sum256([]byte(baseURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]

	// Try to extract repository name from URL
	repoId := "rpm"
	if u, err := url.Parse(baseURL); err == nil {
		pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
		for _, part := range pathParts {
			if part != "" && !strings.Contains(part, ".") {
				repoId = part
				break
			}
		}
	}

	// Detect architecture from URL if possible
	arch := "x86_64" // default
	if strings.Contains(baseURL, "aarch64") {
		arch = "aarch64"
	} else if strings.Contains(baseURL, "i386") {
		arch = "i386"
	} else if strings.Contains(baseURL, "armhf") {
		arch = "armhf"
	}

	return fmt.Sprintf("%s_%s_%s_rpm", repoId, arch, urlHashStr)
}

// saveOriginalXML saves the original compressed XML file to cache directory
func saveOriginalXML(xmlCacheDir, gzHref, baseURL string, data []byte) {
	log := logger.Logger()

	// Generate filename from URL and timestamp
	urlHash := sha256.Sum256([]byte(baseURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]
	timestamp := time.Now().Format("2006-01-02_15-04-05")

	baseFilename := strings.TrimSuffix(filepath.Base(gzHref), filepath.Ext(gzHref))
	filename := fmt.Sprintf("%s_%s_%s%s", baseFilename, urlHashStr, timestamp, filepath.Ext(gzHref))

	filePath := filepath.Join(xmlCacheDir, filename)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Warnf("Failed to save original XML file %s: %v", filePath, err)
		return
	}

	log.Infof("Saved original XML file: %s", filePath)
}

// saveUncompressedXML saves the uncompressed XML content to cache directory
func saveUncompressedXML(xmlCacheDir, gzHref, baseURL string, xmlData []byte) {
	log := logger.Logger()

	// Generate filename from URL and timestamp
	urlHash := sha256.Sum256([]byte(baseURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]
	timestamp := time.Now().Format("2006-01-02_15-04-05")

	baseFilename := strings.TrimSuffix(filepath.Base(gzHref), filepath.Ext(gzHref))
	filename := fmt.Sprintf("%s_%s_%s.xml", baseFilename, urlHashStr, timestamp)

	filePath := filepath.Join(xmlCacheDir, filename)
	if err := os.WriteFile(filePath, xmlData, 0644); err != nil {
		log.Warnf("Failed to save uncompressed XML file %s: %v", filePath, err)
		return
	}

	log.Infof("Saved uncompressed XML file: %s", filePath)
}
