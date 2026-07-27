package selfupdate

import "testing"

func TestAssetNameReleaseMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   string
	}{
		{
			name:   "darwin amd64",
			goos:   "darwin",
			goarch: "amd64",
			want:   "uddns_1.9.0_darwin_amd64.zip",
		},
		{
			name:   "darwin arm64",
			goos:   "darwin",
			goarch: "arm64",
			want:   "uddns_1.9.0_darwin_arm64.zip",
		},
		{
			name:   "linux amd64",
			goos:   "linux",
			goarch: "amd64",
			want:   "uddns_1.9.0_linux_amd64.tar.gz",
		},
		{
			name:   "linux arm v7",
			goos:   "linux",
			goarch: "arm",
			want:   "uddns_1.9.0_linux_armv7.tar.gz",
		},
		{
			name:   "linux arm64",
			goos:   "linux",
			goarch: "arm64",
			want:   "uddns_1.9.0_linux_arm64.tar.gz",
		},
		{
			name:   "freebsd amd64",
			goos:   "freebsd",
			goarch: "amd64",
			want:   "uddns_1.9.0_freebsd_amd64.zip",
		},
		{
			name:   "windows amd64",
			goos:   "windows",
			goarch: "amd64",
			want:   "uddns_1.9.0_windows_amd64.zip",
		},
		{
			name:   "windows arm64",
			goos:   "windows",
			goarch: "arm64",
			want:   "uddns_1.9.0_windows_arm64.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := assetName("uddns", "1.9.0", tt.goos, tt.goarch)
			if err != nil {
				t.Fatalf("assetName returned unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("assetName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAssetNameAcceptsArtifactComponents(t *testing.T) {
	t.Parallel()
	got, err := assetName("ud-dns", "1.9.0-rc.1", "linux", "amd64")
	if err != nil {
		t.Fatalf("assetName returned unexpected error: %v", err)
	}
	want := "ud-dns_1.9.0-rc.1_linux_amd64.tar.gz"
	if got != want {
		t.Fatalf("assetName = %q, want %q", got, want)
	}
}

func TestAssetNameRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		project string
		version string
		goos    string
		goarch  string
	}{
		{
			name:    "empty project",
			project: "",
			version: "1.9.0",
			goos:    "linux",
			goarch:  "amd64",
		},
		{
			name:    "project with slash",
			project: "owner/uddns",
			version: "1.9.0",
			goos:    "linux",
			goarch:  "amd64",
		},
		{
			name:    "project with backslash",
			project: `owner\uddns`,
			version: "1.9.0",
			goos:    "windows",
			goarch:  "amd64",
		},
		{
			name:    "project dot path",
			project: ".",
			version: "1.9.0",
			goos:    "linux",
			goarch:  "amd64",
		},
		{
			name:    "empty version",
			project: "uddns",
			version: "",
			goos:    "linux",
			goarch:  "amd64",
		},
		{
			name:    "version with slash",
			project: "uddns",
			version: "1.9.0/evil",
			goos:    "linux",
			goarch:  "amd64",
		},
		{
			name:    "version with backslash",
			project: "uddns",
			version: `1.9.0\evil`,
			goos:    "windows",
			goarch:  "amd64",
		},
		{
			name:    "version with leading v",
			project: "uddns",
			version: "v1.9.0",
			goos:    "linux",
			goarch:  "amd64",
		},
		{
			name:    "unsupported operating system",
			project: "uddns",
			version: "1.9.0",
			goos:    "openbsd",
			goarch:  "amd64",
		},
		{
			name:    "unsupported linux architecture",
			project: "uddns",
			version: "1.9.0",
			goos:    "linux",
			goarch:  "386",
		},
		{
			name:    "artifact arm spelling is not GOARCH",
			project: "uddns",
			version: "1.9.0",
			goos:    "linux",
			goarch:  "armv7",
		},
		{
			name:    "unsupported darwin architecture",
			project: "uddns",
			version: "1.9.0",
			goos:    "darwin",
			goarch:  "386",
		},
		{
			name:    "unsupported freebsd architecture",
			project: "uddns",
			version: "1.9.0",
			goos:    "freebsd",
			goarch:  "arm64",
		},
		{
			name:    "unsupported windows architecture",
			project: "uddns",
			version: "1.9.0",
			goos:    "windows",
			goarch:  "386",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := assetName(
				tt.project,
				tt.version,
				tt.goos,
				tt.goarch,
			)
			if err == nil {
				t.Fatalf("assetName = %q, want an error", got)
			}
		})
	}
}

func TestArchiveExecutableName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		project string
		goos    string
		want    string
	}{
		{
			name:    "windows",
			project: "uddns",
			goos:    "windows",
			want:    "uddns.exe",
		},
		{
			name:    "linux",
			project: "uddns",
			goos:    "linux",
			want:    "uddns",
		},
		{
			name:    "darwin",
			project: "uddns",
			goos:    "darwin",
			want:    "uddns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := archiveExecutableName(tt.project, tt.goos); got != tt.want {
				t.Fatalf(
					"archiveExecutableName(%q, %q) = %q, want %q",
					tt.project,
					tt.goos,
					got,
					tt.want,
				)
			}
		})
	}
}
