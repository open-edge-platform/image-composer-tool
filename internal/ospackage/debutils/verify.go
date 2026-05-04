package debutils

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/schollz/progressbar/v3"
)

// Result holds the outcome of verifying one RPM.
type Result struct {
	Path     string        // filesystem path to the .rpm
	OK       bool          // signature + checksum OK?
	Duration time.Duration // how long the check took
	Error    error         // any error (signature fail, I/O, etc)
}

// isBinaryGPGKey checks if the data appears to be a binary GPG key
func isBinaryGPGKey(data []byte) bool {
	// Check for ASCII armored format first
	if bytes.HasPrefix(data, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")) {
		return false // This is ASCII armored, not binary
	}

	// Try to parse as OpenPGP packet to determine if it's binary
	reader := bytes.NewReader(data)
	_, err := openpgp.ReadKeyRing(reader)
	if err == nil {
		return true // Successfully parsed as binary OpenPGP
	}

	// Additional heuristic: if it contains mostly non-printable characters
	if len(data) < 4 {
		return false
	}

	printableCount := 0
	checkLength := len(data)
	if checkLength > 100 {
		checkLength = 100
	}

	for i := 0; i < checkLength; i++ {
		if data[i] >= 32 && data[i] <= 126 {
			printableCount++
		}
	}

	// If less than 70% printable characters, likely binary
	return float64(printableCount)/float64(checkLength) < 0.7
}

// convertBinaryGPGToAscii converts binary GPG key to ASCII armored format using Go crypto
func convertBinaryGPGToAscii(binaryData []byte) ([]byte, error) {
	// Try to parse the binary data as an OpenPGP key ring
	reader := bytes.NewReader(binaryData)
	keyRing, err := openpgp.ReadKeyRing(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse binary GPG key: %w", err)
	}

	var armoredBuf bytes.Buffer

	// Create ASCII armor encoder
	armorWriter, err := armor.Encode(&armoredBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create armor encoder: %w", err)
	}

	// Serialize each entity in the keyring
	for _, entity := range keyRing {
		if err := entity.Serialize(armorWriter); err != nil {
			armorWriter.Close()
			return nil, fmt.Errorf("failed to serialize key entity: %w", err)
		}
	}

	if err := armorWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close armor encoder: %w", err)
	}

	return armoredBuf.Bytes(), nil
}

func VerifyPackagegz(relPath string, pkggzPath string, arch string, component string) (bool, error) {
	log := logger.Logger()
	log.Infof("Verifying package %s", pkggzPath)

	// Get the base filename and determine what to look for in Release file
	baseFile := filepath.Base(pkggzPath)

	// Get expected checksum from Release file
	pkgPathSrch := fmt.Sprintf("%s/binary-%s/%s", component, arch, baseFile)
	log.Infof("Searching for %s in Release file %s", pkgPathSrch, relPath)
	checksum, err := findChecksumInRelease(relPath, "SHA256", pkgPathSrch)
	log.Infof("Checksum from Release file (%s): %s Err:%s", relPath, checksum, err)
	if err != nil {
		return false, fmt.Errorf("failed to get checksum from Release: %w", err)
	}

	// Compute actual checksum of Packages.gz
	actual, err := computeFileSHA256(pkggzPath)
	if err != nil {
		return false, fmt.Errorf("failed to compute checksum for %s: %w", pkggzPath, err)
	}

	// Compare
	if !strings.EqualFold(actual, checksum) {
		log.Errorf("Checksum mismatch: expected %s, got %s", checksum, actual)
		return false, fmt.Errorf("checksum mismatch: expected %s, got %s", checksum, actual)
	}

	log.Infof("Checksum verified successfully for %s", pkggzPath)
	return true, nil
}

func VerifyRelease(relPath string, relSignPath string, pKeyPath string) (bool, error) {
	log := logger.Logger()

	//ignore verification if trusted=yes
	if pKeyPath == "[trusted=yes]" {
		log.Infof("Repository marked (%s) as [trusted=yes], skipping Release file signature verification", relPath)
		return true, nil
	}

	// Read the public key
	keyringBytes, err := os.ReadFile(pKeyPath)
	if err != nil {
		return false, fmt.Errorf("failed to read public key: %w", err)
	}

	// Check if the key file is a binary GPG key and convert if needed
	if isBinaryGPGKey(keyringBytes) {
		log.Infof("GPG key %s is binary format, converting to ASCII armored format", pKeyPath)
		convertedBytes, err := convertBinaryGPGToAscii(keyringBytes)
		if err != nil {
			log.Warnf("Failed to convert binary GPG key to ASCII: %v, trying original data", err)
		} else {
			keyringBytes = convertedBytes
			log.Infof("Successfully converted binary GPG key to ASCII armored format")
		}
	} else {
		log.Infof("GPG key data appears to be ASCII armored already or is a standard key format")
	}

	// Read the Release file and its signature
	release, err := os.ReadFile(relPath)
	if err != nil {
		return false, fmt.Errorf("failed to read Release file: %w", err)
	}
	signature, err := os.ReadFile(relSignPath)
	if err != nil {
		return false, fmt.Errorf("failed to read Release signature: %w", err)
	}

	// Try to import the public key - support both binary and armored formats
	var keyring openpgp.EntityList

	// First try as armored key (text format)
	keyring, err = openpgp.ReadArmoredKeyRing(bytes.NewReader(keyringBytes))
	if err != nil {
		log.Infof("Failed to parse as armored key, trying binary format: %v", err)
		// Try as binary key format
		keyring, err = openpgp.ReadKeyRing(bytes.NewReader(keyringBytes))
		if err != nil {
			return false, fmt.Errorf("failed to parse public key (tried both armored and binary formats): %w", err)
		}
	}

	// Check if signature is binary or armored and verify accordingly
	sigReader := bytes.NewReader(signature)
	releaseReader := bytes.NewReader(release)

	// Try armored signature first
	_, err = openpgp.CheckArmoredDetachedSignature(
		openpgp.EntityList(keyring),
		releaseReader,
		sigReader,
		&packet.Config{},
	)

	if err != nil {
		log.Infof("Failed to verify as armored signature, trying binary format: %v", err)
		// Reset readers
		sigReader = bytes.NewReader(signature)
		releaseReader = bytes.NewReader(release)

		// Try binary signature
		_, err = openpgp.CheckDetachedSignature(
			openpgp.EntityList(keyring),
			releaseReader,
			sigReader,
			&packet.Config{},
		)

		if err != nil {
			if strings.Contains(err.Error(), "unknown entity") || strings.Contains(err.Error(), "signature made by unknown entity") {
				log.Warnf("Signature verification failed due to unknown entity, but allowing: %v", err)
				return true, nil
			}
			return false, fmt.Errorf("signature verification failed (tried both armored and binary): %w", err)
		}
	}

	log.Infof("Release file verified successfully")
	return true, nil
}

// VerifyAll takes a slice of DEB file paths, verifies each one in parallel,
// and returns a slice of results in the same order.
func VerifyDEBs(paths []string, pkgChecksum map[string][]string, workers int) []Result {
	log := logger.Logger()

	log.Infof("Verifying %d packages with %d workers", len(paths), workers)

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
		go func(workerIdx int) {
			defer wg.Done()
			for idx := range jobs {
				debPath := paths[idx]
				name := filepath.Base(debPath)
				bar.Describe("verifying " + name)

				start := time.Now()

				var err error
				checksums, ok := pkgChecksum[filepath.Base(debPath)]
				if !ok || len(checksums) == 0 {
					err = fmt.Errorf("no checksums found for package %s", debPath)
				} else {
					for _, checksum := range checksums {
						// retry verification with each checksum saved for debPath
						// (there can be multiple checksums for the same package name)
						err = verifyWithGoDeb(debPath, map[string]string{filepath.Base(debPath): checksum})
						if err == nil {
							break // stop at first successful verification
						}
					}
				}
				ok = err == nil

				if err != nil {
					log.Errorf("verification %s failed: %v", debPath, err)
				}

				results[idx] = Result{
					Path:     debPath,
					OK:       ok,
					Duration: time.Since(start),
					Error:    err,
				}

				if err := bar.Add(1); err != nil {
					log.Errorf("failed to add to progress bar: %v", err)
				}
			}
		}(i)
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

// checksumWithGoDeb verifies the checksum of a .deb file using the GoDeb library.
func verifyWithGoDeb(deb string, pkgChecksum map[string]string) error {

	checksum := getChecksumByName(pkgChecksum, deb)

	// Here you would implement the actual checksum verification logic
	if checksum == "NOT FOUND" {
		return fmt.Errorf("no checksum found for %s", deb)
	}

	actual, err := computeFileSHA256(deb)
	if err != nil {
		return fmt.Errorf("failed to compute checksum for %s: %w", deb, err)
	}

	if !strings.EqualFold(actual, checksum) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", deb, checksum, actual)
	}

	return nil
}

func getChecksumByName(pkgChecksum map[string]string, deb string) string {
	base := filepath.Base(deb)
	if checksum, ok := pkgChecksum[base]; ok {
		return checksum
	}
	return "NOT FOUND"
}

// computeFileSHA256 computes the SHA256 checksum of the given file.
func computeFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// FindChecksumInRelease parses the Release file and returns the checksum for the given file and checksum type.
// Example: findChecksumInRelease("Release", "SHA256", "main/binary-amd64/Packages.gz")
func findChecksumInRelease(releasePath, checksumType, fileName string) (string, error) {
	f, err := os.Open(releasePath)
	if err != nil {
		return "", fmt.Errorf("failed to open release file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inChecksumSection := false

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Check for the start of the checksum section
		if strings.HasSuffix(line, ":") && strings.EqualFold(strings.TrimSuffix(line, ":"), checksumType) {
			inChecksumSection = true
			continue
		}

		// If we are in the checksum section, look for the file
		if inChecksumSection {
			// End of section if we hit a new section header or blank line
			if line == "" || strings.HasSuffix(line, ":") {
				break
			}

			parts := strings.Fields(line)
			if len(parts) < 3 {
				continue
			}
			if parts[2] == fileName {
				return parts[0], nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading release file: %v", err)
	}

	log := logger.Logger()
	log.Warnf("Could not find %s in section %s of %s", fileName, checksumType, releasePath)
	return "", fmt.Errorf("checksum for %s (%s) not found", fileName, checksumType)
}
