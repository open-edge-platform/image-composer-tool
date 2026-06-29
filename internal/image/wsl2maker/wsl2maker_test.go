package wsl2maker

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestArchiveFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		artifact    config.ArtifactInfo
		wantType    string
		wantExt     string
		expectError bool
	}{
		{name: "gzip", artifact: config.ArtifactInfo{Type: "tar", Compression: "gz"}, wantType: "tar.gz", wantExt: "tar.gz"},
		{name: "xz", artifact: config.ArtifactInfo{Type: "tar", Compression: "xz"}, wantType: "tar.xz", wantExt: "tar.xz"},
		{name: "gzip alias", artifact: config.ArtifactInfo{Type: "tar", Compression: "gzip"}, wantType: "tar.gz", wantExt: "tar.gz"},
		{name: "unsupported compression", artifact: config.ArtifactInfo{Type: "tar", Compression: "zstd"}, expectError: true},
		{name: "unsupported type", artifact: config.ArtifactInfo{Type: "raw", Compression: "gz"}, expectError: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			template := &config.ImageTemplate{
				Disk: config.DiskConfig{Artifacts: []config.ArtifactInfo{tt.artifact}},
			}

			gotType, gotExt, err := archiveFormat(template)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("archiveFormat() error = %v", err)
			}
			if gotType != tt.wantType || gotExt != tt.wantExt {
				t.Fatalf("archiveFormat() = %s, %s; want %s, %s", gotType, gotExt, tt.wantType, tt.wantExt)
			}
		})
	}
}

func TestNewWSL2MakerNilChrootEnv(t *testing.T) {
	t.Parallel()

	template := &config.ImageTemplate{}
	if _, err := NewWSL2Maker(nil, template); err == nil {
		t.Fatal("expected error")
	}
}
