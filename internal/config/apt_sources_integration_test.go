package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createLocalTestGPGKey(t *testing.T, pattern string) string {
	t.Helper()

	tempFile, err := os.CreateTemp("", pattern)
	if err != nil {
		t.Fatalf("Failed to create local test GPG key file: %v", err)
	}

	if _, err := tempFile.WriteString("dummy-gpg-key-content"); err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		t.Fatalf("Failed to write local test GPG key file: %v", err)
	}

	if err := tempFile.Close(); err != nil {
		os.Remove(tempFile.Name())
		t.Fatalf("Failed to close local test GPG key file: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Remove(tempFile.Name())
	})

	return tempFile.Name()
}

// resolveAdditionalFilePath converts relative paths (like ../../../../../../tmp/file.gpg)
// to absolute paths by joining with working directory or config root
func resolveAdditionalFilePath(relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) {
		return relativePath, nil
	}

	// If the path starts with ../../../../../../tmp/, extract filename and look in ./tmp
	if strings.HasPrefix(relativePath, filepath.Join("..", "..", "..", "..", "..", "..", "tmp")) {
		// Get current working directory
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}

		// Extract just the filename from the relative path
		filename := filepath.Base(relativePath)
		// Return path in local ./tmp relative to cwd
		return filepath.Join(wd, "tmp", filename), nil
	}

	// For other relative paths, join with working directory
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(wd, relativePath)), nil
}

// TestIntegrationAptSourcesGeneration tests the complete flow
func TestIntegrationAptSourcesGeneration(t *testing.T) {
	sedKeyPath := createLocalTestGPGKey(t, "sed-test-key-*.gpg")
	openvinoKeyPath := createLocalTestGPGKey(t, "openvino-test-key-*.gpg")

	// Create a realistic test template similar to the example
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-package-repos-ubuntu",
			Version: "24.04",
		},
		Target: TargetInfo{
			OS:        "ubuntu",
			Dist:      "ubuntu24",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		PackageRepositories: []PackageRepository{
			{
				Codename:  "sed",
				URL:       "https://eci.intel.com/sed-repos/noble",
				PKey:      sedKeyPath,
				Priority:  1000,
				Component: "",
			},
			{
				Codename:  "ubuntu24",
				URL:       "https://apt.repos.intel.com/openvino/2025",
				PKey:      openvinoKeyPath,
				Component: "main contrib",
			},
		},
		SystemConfig: SystemConfig{
			Name: "test-minimal",
			AdditionalFiles: []AdditionalFileInfo{
				{Local: "../additionalfiles/dhcp.network", Final: "/etc/systemd/network/dhcp.network"},
			},
		},
	}

	// Test the generation
	err := template.GenerateAptSourcesFromRepositories()
	if err != nil {
		t.Fatalf("Failed to generate apt sources: %v", err)
	}

	// Verify additional file was added
	foundAptSources := false
	var aptSourcesFile AdditionalFileInfo
	for _, file := range template.SystemConfig.AdditionalFiles {
		if file.Final == "/etc/apt/sources.list.d/package-repositories.list" {
			foundAptSources = true
			aptSourcesFile = file
			break
		}
	}

	if !foundAptSources {
		t.Fatal("Apt sources file was not added to additionalFiles")
	}

	aptSourcesAbsPath, err := resolveAdditionalFilePath(aptSourcesFile.Local)
	if err != nil {
		t.Fatalf("Failed to resolve apt sources file path: %v", err)
	}

	// Verify the file exists and has correct content
	if _, err := os.Stat(aptSourcesAbsPath); os.IsNotExist(err) {
		t.Fatalf("Generated apt sources file does not exist: %s", aptSourcesFile.Local)
	}

	// Clean up
	defer os.Remove(aptSourcesAbsPath)

	// Read and verify content
	content, err := os.ReadFile(aptSourcesAbsPath)
	if err != nil {
		t.Fatalf("Failed to read apt sources file: %v", err)
	}

	contentStr := string(content)

	// Check for expected content - simple deb lines without signed-by directives
	expectedLines := []string{
		"deb https://eci.intel.com/sed-repos/noble sed main",
		"deb https://apt.repos.intel.com/openvino/2025 ubuntu24 main contrib",
	}

	for _, expectedLine := range expectedLines {
		if !strings.Contains(contentStr, expectedLine) {
			t.Errorf("Generated apt sources file missing expected line: %q\nActual content:\n%s", expectedLine, contentStr)
		}
	}

	t.Logf("Successfully generated apt sources file with content:\n%s", contentStr)
}

// TestIntegrationAptPreferencesGeneration tests the complete flow including preferences
func TestIntegrationAptPreferencesGeneration(t *testing.T) {
	sedKeyPath := createLocalTestGPGKey(t, "sed-test-key-*.gpg")
	openvinoKeyPath := createLocalTestGPGKey(t, "openvino-test-key-*.gpg")

	// Create a realistic test template with priorities
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-package-repos-with-priorities",
			Version: "24.04",
		},
		Target: TargetInfo{
			OS:        "ubuntu",
			Dist:      "ubuntu24",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		PackageRepositories: []PackageRepository{
			{
				ID:       "sed-repo",
				Codename: "sed",
				URL:      "https://eci.intel.com/sed-repos/noble",
				PKey:     sedKeyPath,
				Priority: 1000,
			},
			{
				ID:        "openvino-repo",
				Codename:  "ubuntu24",
				URL:       "https://apt.repos.intel.com/openvino/2025",
				PKey:      openvinoKeyPath,
				Component: "main contrib",
				Priority:  500,
			},
			{
				// Repository without priority should get default 500 and generate preferences file
				Codename: "no-priority-repo",
				URL:      "https://example.com/repo",
			},
		},
		SystemConfig: SystemConfig{
			Name:            "test-minimal",
			AdditionalFiles: []AdditionalFileInfo{},
		},
	}

	// Test the generation
	err := template.GenerateAptSourcesFromRepositories()
	if err != nil {
		t.Fatalf("Failed to generate apt sources and preferences: %v", err)
	}

	// Should have: 1 sources file + 3 preferences files + 2 GPG keys = 6 additional files
	expectedFileCount := 6
	if len(template.SystemConfig.AdditionalFiles) != expectedFileCount {
		t.Errorf("Expected %d additional files, got %d", expectedFileCount, len(template.SystemConfig.AdditionalFiles))
	}

	// Verify files exist and have correct content
	var sourcesFile, sedPrefsFile, openvinoPrefsFile, noPriorityPrefsFile *AdditionalFileInfo
	gpgKeyCount := 0

	for i, file := range template.SystemConfig.AdditionalFiles {
		switch {
		case file.Final == "/etc/apt/sources.list.d/package-repositories.list":
			sourcesFile = &template.SystemConfig.AdditionalFiles[i]
		case file.Final == "/etc/apt/preferences.d/sed-repo":
			sedPrefsFile = &template.SystemConfig.AdditionalFiles[i]
		case file.Final == "/etc/apt/preferences.d/openvino-repo":
			openvinoPrefsFile = &template.SystemConfig.AdditionalFiles[i]
		case strings.Contains(file.Final, "no-priority-repo"):
			// Updated: filename now includes URL domain for uniqueness
			noPriorityPrefsFile = &template.SystemConfig.AdditionalFiles[i]
		case strings.HasPrefix(file.Final, "/etc/apt/trusted.gpg.d/"):
			gpgKeyCount++
		}
	}

	// Clean up all temp files
	defer func() {
		for _, file := range template.SystemConfig.AdditionalFiles {
			absPath, _ := resolveAdditionalFilePath(file.Local)
			os.Remove(absPath)
		}
	}()

	// Verify sources file
	if sourcesFile == nil {
		t.Fatal("Sources file not found in additionalFiles")
	}

	sourcesAbsPath, err := resolveAdditionalFilePath(sourcesFile.Local)
	if err != nil {
		t.Fatalf("Failed to resolve sources file path: %v", err)
	}

	sourcesContent, err := os.ReadFile(sourcesAbsPath)
	if err != nil {
		t.Fatalf("Failed to read sources file: %v", err)
	}

	sourcesStr := string(sourcesContent)
	if !strings.Contains(sourcesStr, "deb https://eci.intel.com/sed-repos/noble sed main") {
		t.Error("Sources file missing expected SED repository line")
	}

	// Verify SED preferences file
	if sedPrefsFile == nil {
		t.Fatal("SED preferences file not found in additionalFiles")
	}

	sedAbsPath, err := resolveAdditionalFilePath(sedPrefsFile.Local)
	if err != nil {
		t.Fatalf("Failed to resolve SED preferences file path: %v", err)
	}

	sedContent, err := os.ReadFile(sedAbsPath)
	if err != nil {
		t.Fatalf("Failed to read SED preferences file: %v", err)
	}

	expectedSedContent := "# Priority 1000: Install even if version is lower than installed\nPackage: *\nPin: origin eci.intel.com\nPin-Priority: 1000\n"
	if string(sedContent) != expectedSedContent {
		t.Errorf("SED preferences file content mismatch.\nExpected:\n%s\nGot:\n%s", expectedSedContent, string(sedContent))
	}

	// Verify OpenVINO preferences file
	if openvinoPrefsFile == nil {
		t.Fatal("OpenVINO preferences file not found in additionalFiles")
	}

	openvinoAbsPath, err := resolveAdditionalFilePath(openvinoPrefsFile.Local)
	if err != nil {
		t.Fatalf("Failed to resolve OpenVINO preferences file path: %v", err)
	}

	openvinoContent, err := os.ReadFile(openvinoAbsPath)
	if err != nil {
		t.Fatalf("Failed to read OpenVINO preferences file: %v", err)
	}

	expectedOpenvinoContent := "# Priority 500: Default\nPackage: *\nPin: origin apt.repos.intel.com\nPin-Priority: 500\n"
	if string(openvinoContent) != expectedOpenvinoContent {
		t.Errorf("OpenVINO preferences file content mismatch.\nExpected:\n%s\nGot:\n%s", expectedOpenvinoContent, string(openvinoContent))
	}

	// Verify no-priority repo preferences file (should get default 500)
	if noPriorityPrefsFile == nil {
		t.Fatal("No-priority preferences file not found in additionalFiles")
	}

	noPriorityAbsPath, err := resolveAdditionalFilePath(noPriorityPrefsFile.Local)
	if err != nil {
		t.Fatalf("Failed to resolve no-priority preferences file path: %v", err)
	}

	noPriorityContent, err := os.ReadFile(noPriorityAbsPath)
	if err != nil {
		t.Fatalf("Failed to read no-priority preferences file: %v", err)
	}

	expectedNoPriorityContent := "# Priority 500: Default\nPackage: *\nPin: origin example.com\nPin-Priority: 500\n"
	if string(noPriorityContent) != expectedNoPriorityContent {
		t.Errorf("No-priority preferences file content mismatch.\nExpected:\n%s\nGot:\n%s", expectedNoPriorityContent, string(noPriorityContent))
	}

	// Verify GPG keys were downloaded and added
	if gpgKeyCount != 2 {
		t.Errorf("Expected 2 GPG key files, got %d", gpgKeyCount)
	}

	t.Logf("Successfully generated apt sources and preferences files")
	t.Logf("Sources content:\n%s", sourcesStr)
	t.Logf("SED preferences content:\n%s", string(sedContent))
	t.Logf("OpenVINO preferences content:\n%s", string(openvinoContent))
}

// TestIntegrationNoPriorityRepositories tests that preferences files are generated with default 500 priority
func TestIntegrationNoPriorityRepositories(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{
			OS: "ubuntu",
		},
		PackageRepositories: []PackageRepository{
			{
				Codename: "stable",
				URL:      "https://example.com/repo",
				// No priority set - should get default 500
			},
		},
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{},
		},
	}

	err := template.GenerateAptSourcesFromRepositories()
	if err != nil {
		t.Fatalf("Failed to generate apt sources: %v", err)
	}

	// Should have 2 files: 1 sources + 1 preferences with default 500 priority
	if len(template.SystemConfig.AdditionalFiles) != 2 {
		t.Errorf("Expected 2 additional files (sources + preferences), got %d", len(template.SystemConfig.AdditionalFiles))
	}

	// Clean up
	defer func() {
		for _, file := range template.SystemConfig.AdditionalFiles {
			os.Remove(file.Local)
		}
	}()

	// Verify sources and preferences files
	if len(template.SystemConfig.AdditionalFiles) >= 2 {
		var sourcesFile, preferencesFile *AdditionalFileInfo
		for i := range template.SystemConfig.AdditionalFiles {
			file := &template.SystemConfig.AdditionalFiles[i]
			if strings.HasPrefix(file.Final, "/etc/apt/sources.list.d/") {
				sourcesFile = file
			} else if strings.HasPrefix(file.Final, "/etc/apt/preferences.d/") {
				preferencesFile = file
			}
		}

		if sourcesFile == nil {
			t.Error("Sources file not found")
		} else if sourcesFile.Final != "/etc/apt/sources.list.d/package-repositories.list" {
			t.Errorf("Expected sources file, got %s", sourcesFile.Final)
		}

		if preferencesFile == nil {
			t.Error("Preferences file not found")
		} else {
			// Check preferences file content
			prefsAbsPath, err := resolveAdditionalFilePath(preferencesFile.Local)
			if err != nil {
				t.Errorf("Failed to resolve preferences file path: %v", err)
			} else {
				content, err := os.ReadFile(prefsAbsPath)
				if err != nil {
					t.Errorf("Failed to read preferences file: %v", err)
				} else {
					expectedContent := "# Priority 500: Default\nPackage: *\nPin: origin example.com\nPin-Priority: 500\n"
					if string(content) != expectedContent {
						t.Errorf("Preferences file content mismatch.\nExpected:\n%s\nGot:\n%s", expectedContent, string(content))
					}
				}
			}
		}
	}
}

// TestIntegrationRPMSystem tests that nothing happens for RPM-based systems
func TestIntegrationRPMSystem(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{
			OS: "azl", // RPM-based system
		},
		PackageRepositories: []PackageRepository{
			{
				Codename: "stable",
				URL:      "https://example.com/repo",
			},
		},
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{},
		},
	}

	initialFileCount := len(template.SystemConfig.AdditionalFiles)

	err := template.GenerateAptSourcesFromRepositories()
	if err != nil {
		t.Fatalf("Failed to generate apt sources: %v", err)
	}

	finalFileCount := len(template.SystemConfig.AdditionalFiles)
	if finalFileCount != initialFileCount {
		t.Errorf("Expected no additional files for RPM system, got %d additional files", finalFileCount-initialFileCount)
	}
}

// TestIntegrationEmptyRepositories tests behavior with no repositories
func TestIntegrationEmptyRepositories(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{
			OS: "ubuntu",
		},
		PackageRepositories: []PackageRepository{}, // Empty
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{},
		},
	}

	initialFileCount := len(template.SystemConfig.AdditionalFiles)

	err := template.GenerateAptSourcesFromRepositories()
	if err != nil {
		t.Fatalf("Failed to generate apt sources: %v", err)
	}

	finalFileCount := len(template.SystemConfig.AdditionalFiles)
	if finalFileCount != initialFileCount {
		t.Errorf("Expected no additional files for empty repositories, got %d additional files", finalFileCount-initialFileCount)
	}
}

// TestIntegrationWithExistingFile tests that existing apt sources files are replaced
func TestIntegrationWithExistingFile(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{
			OS: "ubuntu",
		},
		PackageRepositories: []PackageRepository{
			{
				Codename: "stable",
				URL:      "https://example.com/repo",
			},
		},
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{
				{Local: "/tmp/existing-sources.list", Final: "/etc/apt/sources.list.d/package-repositories.list"},
			},
		},
	}

	initialFileCount := len(template.SystemConfig.AdditionalFiles)

	err := template.GenerateAptSourcesFromRepositories()
	if err != nil {
		t.Fatalf("Failed to generate apt sources: %v", err)
	}

	// Should now have 2 files (sources replacement + new preferences file)
	finalFileCount := len(template.SystemConfig.AdditionalFiles)
	expectedCount := initialFileCount + 1 // 1 existing sources + 1 new preferences
	if finalFileCount != expectedCount {
		t.Errorf("Expected %d files after replacement and preferences addition, got %d", expectedCount, finalFileCount)
	}

	// Clean up generated temp files
	defer func() {
		for _, file := range template.SystemConfig.AdditionalFiles {
			if strings.HasPrefix(file.Local, "/tmp/") && file.Local != "/tmp/existing-sources.list" {
				os.Remove(file.Local)
			}
		}
	}()

	// Verify the sources file was replaced and preferences file was added
	var sourcesFound, preferencesFound bool
	for _, file := range template.SystemConfig.AdditionalFiles {
		if file.Final == "/etc/apt/sources.list.d/package-repositories.list" {
			if file.Local == "/tmp/existing-sources.list" {
				t.Error("Sources file was not replaced - local path is still the old one")
			}
			sourcesFound = true
		} else if strings.HasPrefix(file.Final, "/etc/apt/preferences.d/") {
			preferencesFound = true
		}
	}

	if !sourcesFound {
		t.Error("Expected apt sources file not found after replacement")
	}
	if !preferencesFound {
		t.Error("Expected preferences file not found after addition")
	}
}
