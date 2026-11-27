package debutils

import "testing"

func TestParseBinarySourceIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, binary, version, declaration string
		hasDeclaration                     bool
		wantName, wantVersion              string
	}{
		{name: "missing", binary: "mesa-bin", version: "1.0-1", wantName: "mesa-bin", wantVersion: "1.0-1"},
		{name: "implicit", binary: "mesa-bin", version: "1.0-1", declaration: "mesa", hasDeclaration: true, wantName: "mesa", wantVersion: "1.0-1"},
		{name: "explicit epoch", binary: "mesa-bin", version: "1.0-1+b1", declaration: "mesa (2:26.0.8-1ppa1~resolute1)", hasDeclaration: true, wantName: "mesa", wantVersion: "2:26.0.8-1ppa1~resolute1"},
		{name: "binNMU", binary: "mesa-bin", version: "1.0-1+b1", declaration: "mesa (1.0-1)", hasDeclaration: true, wantName: "mesa", wantVersion: "1.0-1"},
		{name: "malformed", binary: "mesa-bin", version: "1.0", declaration: "mesa (1.0", hasDeclaration: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name, version := parseBinarySourceIdentity(tt.binary, tt.version, tt.declaration, tt.hasDeclaration)
			if name != tt.wantName || version != tt.wantVersion {
				t.Errorf("parseBinarySourceIdentity(%q, %q, %q, %v) = %q, %q; want %q, %q",
					tt.binary, tt.version, tt.declaration, tt.hasDeclaration, name, version, tt.wantName, tt.wantVersion)
			}
		})
	}
}

func TestParsedPackageCacheVersionIncludesSourceIdentity(t *testing.T) {
	t.Parallel()
	if parsedPackageCacheVersion < 8 {
		t.Errorf("parsedPackageCacheVersion = %d, want at least 8", parsedPackageCacheVersion)
	}
}
