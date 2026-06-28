package provider

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestWSL2ArchiveFormat(t *testing.T) {
	tests := []struct {
		name        string
		artifact    config.ArtifactInfo
		wantType    string
		wantExt     string
		expectError bool
	}{
		{
			name:     "gzip",
			artifact: config.ArtifactInfo{Type: "tar", Compression: "gz"},
			wantType: "tar.gz",
			wantExt:  "tar.gz",
		},
		{
			name:     "xz",
			artifact: config.ArtifactInfo{Type: "tar", Compression: "xz"},
			wantType: "tar.xz",
			wantExt:  "tar.xz",
		},
		{
			name:     "gzip alias",
			artifact: config.ArtifactInfo{Type: "tar", Compression: "gzip"},
			wantType: "tar.gz",
			wantExt:  "tar.gz",
		},
		{
			name:        "unsupported compression",
			artifact:    config.ArtifactInfo{Type: "tar", Compression: "zstd"},
			expectError: true,
		},
		{
			name:        "unsupported type",
			artifact:    config.ArtifactInfo{Type: "raw", Compression: "gz"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template := &config.ImageTemplate{
				Disk: config.DiskConfig{
					Artifacts: []config.ArtifactInfo{tt.artifact},
				},
			}

			gotType, gotExt, err := WSL2ArchiveFormat(template)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("WSL2ArchiveFormat() error = %v", err)
			}
			if gotType != tt.wantType || gotExt != tt.wantExt {
				t.Fatalf("WSL2ArchiveFormat() = %s, %s; want %s, %s",
					gotType, gotExt, tt.wantType, tt.wantExt)
			}
		})
	}
}

func TestBuildWSL2ImageNilChrootEnv(t *testing.T) {
	template := &config.ImageTemplate{}
	if err := BuildWSL2Image(nil, template); err == nil {
		t.Fatal("expected error")
	}
}
