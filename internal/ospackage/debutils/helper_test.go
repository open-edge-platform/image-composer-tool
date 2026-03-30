package debutils

import (
	"regexp"
	"strings"
	"testing"
)

func TestGenerateSPDXFileName(t *testing.T) {
	tests := []struct {
		name   string
		repoNm string
	}{
		{
			name:   "simple repository name",
			repoNm: "Ubuntu",
		},
		{
			name:   "repository name with spaces",
			repoNm: "Azure Linux 3.0",
		},
		{
			name:   "empty repository name",
			repoNm: "",
		},
		{
			name:   "repository name with spaces",
			repoNm: "Test Repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateSPDXFileName(tt.repoNm)
			expectedRepoName := strings.ReplaceAll(tt.repoNm, " ", "_")
			if !strings.Contains(result, expectedRepoName) {
				t.Errorf("GenerateSPDXFileName() = %v, expected to contain %v", result, expectedRepoName)
			}

			// Check that result starts with correct prefix
			if !strings.HasPrefix(result, "spdx_manifest_deb_") {
				t.Errorf("GenerateSPDXFileName() = %v, expected to start with 'spdx_manifest_deb_'", result)
			}

			// Check that result ends with .json
			if !strings.HasSuffix(result, ".json") {
				t.Errorf("GenerateSPDXFileName() = %v, expected to end with '.json'", result)
			}

			// Check that spaces are replaced with underscores
			if !strings.Contains(result, expectedRepoName) {
				t.Errorf("GenerateSPDXFileName() = %v, expected to contain %v", result, expectedRepoName)
			}

			// Check timestamp suffix format
			re := regexp.MustCompile(`^spdx_manifest_deb_.*_[0-9]{8}_[0-9]{6}\.json$`)
			if !re.MatchString(result) {
				t.Errorf("GenerateSPDXFileName() result %q does not match timestamped format", result)
			}
		})
	}
}
