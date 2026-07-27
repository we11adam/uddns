package selfupdate

import "testing"

func TestParseSemanticVersion(t *testing.T) {
	huge := "1234567890123456789012345678901234567890.2.3"
	tests := []struct {
		name           string
		input          string
		wantCanonical  string
		wantArtifact   string
		wantPrerelease bool
	}{
		{
			name:          "minimum",
			input:         "0.0.0",
			wantCanonical: "v0.0.0",
			wantArtifact:  "0.0.0",
		},
		{
			name:          "optional v prefix",
			input:         "v1.2.3",
			wantCanonical: "v1.2.3",
			wantArtifact:  "1.2.3",
		},
		{
			name:           "prerelease and build",
			input:          "1.2.3-alpha.1+build.001",
			wantCanonical:  "v1.2.3-alpha.1+build.001",
			wantArtifact:   "1.2.3-alpha.1+build.001",
			wantPrerelease: true,
		},
		{
			name:           "mixed identifier characters",
			input:          "v10.20.30-0A.a-B--.01a+linux-amd64.000",
			wantCanonical:  "v10.20.30-0A.a-B--.01a+linux-amd64.000",
			wantArtifact:   "10.20.30-0A.a-B--.01a+linux-amd64.000",
			wantPrerelease: true,
		},
		{
			name:          "arbitrarily large core number",
			input:         huge,
			wantCanonical: "v" + huge,
			wantArtifact:  huge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := parseSemanticVersion(tt.input)
			if err != nil {
				t.Fatalf("parseSemanticVersion(%q) returned error: %v", tt.input, err)
			}
			if got := version.canonical(); got != tt.wantCanonical {
				t.Errorf("canonical() = %q, want %q", got, tt.wantCanonical)
			}
			if got := version.artifactVersion(); got != tt.wantArtifact {
				t.Errorf("artifactVersion() = %q, want %q", got, tt.wantArtifact)
			}
			if got := version.isPrerelease(); got != tt.wantPrerelease {
				t.Errorf("isPrerelease() = %v, want %v", got, tt.wantPrerelease)
			}
		})
	}
}

func TestParseSemanticVersionRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "space", input: " "},
		{name: "leading whitespace", input: " 1.2.3"},
		{name: "trailing whitespace", input: "1.2.3 "},
		{name: "internal whitespace", input: "1. 2.3"},
		{name: "prefix only", input: "v"},
		{name: "uppercase prefix", input: "V1.2.3"},
		{name: "double prefix", input: "vv1.2.3"},
		{name: "major only", input: "1"},
		{name: "missing patch", input: "1.2"},
		{name: "extra core identifier", input: "1.2.3.4"},
		{name: "empty major", input: ".2.3"},
		{name: "empty minor", input: "1..3"},
		{name: "empty patch", input: "1.2."},
		{name: "major leading zero", input: "01.2.3"},
		{name: "minor leading zero", input: "1.02.3"},
		{name: "patch leading zero", input: "1.2.03"},
		{name: "negative major", input: "-1.2.3"},
		{name: "signed major", input: "+1.2.3"},
		{name: "non numeric core", input: "1.x.3"},
		{name: "empty prerelease", input: "1.2.3-"},
		{name: "empty prerelease identifier", input: "1.2.3-alpha..1"},
		{name: "numeric prerelease leading zero", input: "1.2.3-01"},
		{name: "numeric prerelease multiple zeros", input: "1.2.3-00"},
		{name: "illegal prerelease underscore", input: "1.2.3-alpha_beta"},
		{name: "non ASCII prerelease", input: "1.2.3-β"},
		{name: "empty build", input: "1.2.3+"},
		{name: "empty build identifier", input: "1.2.3+build..1"},
		{name: "illegal build underscore", input: "1.2.3+build_meta"},
		{name: "non ASCII build", input: "1.2.3+构建"},
		{name: "multiple build separators", input: "1.2.3+build+again"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if version, err := parseSemanticVersion(tt.input); err == nil {
				t.Fatalf("parseSemanticVersion(%q) = %#v, want error", tt.input, version)
			}
		})
	}
}

func TestCompareSemanticVersions(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "equal", a: "1.2.3", b: "v1.2.3", want: 0},
		{name: "build metadata ignored", a: "1.2.3+one", b: "1.2.3+two", want: 0},
		{name: "major", a: "2.0.0", b: "1.999.999", want: 1},
		{name: "minor", a: "1.10.0", b: "1.9.999", want: 1},
		{name: "patch", a: "1.2.100", b: "1.2.99", want: 1},
		{
			name: "arbitrarily large core identifiers",
			a:    "99999999999999999999999999999999999999.0.0",
			b:    "100000000000000000000000000000000000000.0.0",
			want: -1,
		},
		{name: "release after prerelease", a: "1.0.0", b: "1.0.0-rc.1", want: 1},
		{name: "numeric prerelease before alphanumeric", a: "1.0.0-1", b: "1.0.0-alpha", want: -1},
		{name: "numeric prerelease value", a: "1.0.0-11", b: "1.0.0-2", want: 1},
		{
			name: "arbitrarily large prerelease identifiers",
			a:    "1.0.0-99999999999999999999999999999999999999",
			b:    "1.0.0-100000000000000000000000000000000000000",
			want: -1,
		},
		{name: "lexical prerelease", a: "1.0.0-beta", b: "1.0.0-alpha", want: 1},
		{name: "ASCII lexical prerelease", a: "1.0.0-A", b: "1.0.0-a", want: -1},
		{name: "shorter prerelease list", a: "1.0.0-alpha", b: "1.0.0-alpha.1", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := mustParseSemanticVersion(t, tt.a)
			b := mustParseSemanticVersion(t, tt.b)

			if got := compareSemanticVersions(a, b); got != tt.want {
				t.Errorf("compareSemanticVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			if got := compareSemanticVersions(b, a); got != -tt.want {
				t.Errorf("reverse comparison = %d, want %d", got, -tt.want)
			}
		})
	}
}

func TestCompareSemanticVersionsFollowsSemVerPrecedenceExample(t *testing.T) {
	ordered := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
	}

	for i := range len(ordered) - 1 {
		a := mustParseSemanticVersion(t, ordered[i])
		b := mustParseSemanticVersion(t, ordered[i+1])
		if got := compareSemanticVersions(a, b); got != -1 {
			t.Errorf("compareSemanticVersions(%q, %q) = %d, want -1", ordered[i], ordered[i+1], got)
		}
	}
}

func mustParseSemanticVersion(t *testing.T, value string) semanticVersion {
	t.Helper()

	version, err := parseSemanticVersion(value)
	if err != nil {
		t.Fatalf("parseSemanticVersion(%q) returned error: %v", value, err)
	}
	return version
}
