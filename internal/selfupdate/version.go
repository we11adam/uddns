package selfupdate

import (
	"fmt"
	"strings"
)

type semanticVersion struct {
	core       [3]string
	prerelease []string
	build      []string
}

func parseSemanticVersion(value string) (semanticVersion, error) {
	var version semanticVersion

	if value == "" {
		return version, fmt.Errorf("semantic version must not be empty")
	}
	if strings.TrimSpace(value) != value {
		return version, fmt.Errorf("semantic version must not contain surrounding whitespace")
	}
	value = strings.TrimPrefix(value, "v")
	if value == "" {
		return version, fmt.Errorf("semantic version must include a core version")
	}

	versionPart := value
	if plus := strings.IndexByte(versionPart, '+'); plus >= 0 {
		buildPart := versionPart[plus+1:]
		versionPart = versionPart[:plus]
		if strings.Contains(buildPart, "+") {
			return version, fmt.Errorf("semantic version must contain at most one build separator")
		}

		build, err := parseIdentifiers(buildPart, false)
		if err != nil {
			return version, fmt.Errorf("invalid build metadata: %w", err)
		}
		version.build = build
	}

	corePart := versionPart
	if dash := strings.IndexByte(corePart, '-'); dash >= 0 {
		prereleasePart := corePart[dash+1:]
		corePart = corePart[:dash]

		prerelease, err := parseIdentifiers(prereleasePart, true)
		if err != nil {
			return version, fmt.Errorf("invalid prerelease: %w", err)
		}
		version.prerelease = prerelease
	}

	core := strings.Split(corePart, ".")
	if len(core) != len(version.core) {
		return version, fmt.Errorf("semantic version core must contain major, minor, and patch")
	}
	for i, identifier := range core {
		if err := validateNumericIdentifier(identifier); err != nil {
			return version, fmt.Errorf("invalid core identifier %q: %w", identifier, err)
		}
		version.core[i] = identifier
	}

	return version, nil
}

func parseIdentifiers(value string, rejectNumericLeadingZero bool) ([]string, error) {
	identifiers := strings.Split(value, ".")
	for _, identifier := range identifiers {
		if identifier == "" {
			return nil, fmt.Errorf("identifier must not be empty")
		}

		numeric := true
		for i := range len(identifier) {
			character := identifier[i]
			if !isIdentifierCharacter(character) {
				return nil, fmt.Errorf("identifier %q contains an invalid character", identifier)
			}
			if character < '0' || character > '9' {
				numeric = false
			}
		}
		if rejectNumericLeadingZero && numeric && len(identifier) > 1 && identifier[0] == '0' {
			return nil, fmt.Errorf("numeric identifier %q must not contain a leading zero", identifier)
		}
	}

	return identifiers, nil
}

func validateNumericIdentifier(identifier string) error {
	if identifier == "" {
		return fmt.Errorf("identifier must not be empty")
	}
	if len(identifier) > 1 && identifier[0] == '0' {
		return fmt.Errorf("numeric identifier must not contain a leading zero")
	}
	for i := range len(identifier) {
		if identifier[i] < '0' || identifier[i] > '9' {
			return fmt.Errorf("identifier must contain only ASCII digits")
		}
	}

	return nil
}

func isIdentifierCharacter(character byte) bool {
	return character >= '0' && character <= '9' ||
		character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character == '-'
}

func compareSemanticVersions(a, b semanticVersion) int {
	for i := range a.core {
		if comparison := compareNumericIdentifiers(a.core[i], b.core[i]); comparison != 0 {
			return comparison
		}
	}

	switch {
	case len(a.prerelease) == 0 && len(b.prerelease) == 0:
		return 0
	case len(a.prerelease) == 0:
		return 1
	case len(b.prerelease) == 0:
		return -1
	}

	limit := min(len(a.prerelease), len(b.prerelease))
	for i := 0; i < limit; i++ {
		aIdentifier := a.prerelease[i]
		bIdentifier := b.prerelease[i]
		aNumeric := isNumericIdentifier(aIdentifier)
		bNumeric := isNumericIdentifier(bIdentifier)

		var comparison int
		switch {
		case aNumeric && bNumeric:
			comparison = compareNumericIdentifiers(aIdentifier, bIdentifier)
		case aNumeric:
			comparison = -1
		case bNumeric:
			comparison = 1
		default:
			comparison = strings.Compare(aIdentifier, bIdentifier)
		}
		if comparison != 0 {
			return comparison
		}
	}

	return compareInts(len(a.prerelease), len(b.prerelease))
}

func compareNumericIdentifiers(a, b string) int {
	if len(a) != len(b) {
		return compareInts(len(a), len(b))
	}
	return strings.Compare(a, b)
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func isNumericIdentifier(identifier string) bool {
	for i := range len(identifier) {
		if identifier[i] < '0' || identifier[i] > '9' {
			return false
		}
	}
	return true
}

func (v semanticVersion) canonical() string {
	return "v" + v.artifactVersion()
}

func (v semanticVersion) artifactVersion() string {
	var version strings.Builder
	version.WriteString(v.core[0])
	version.WriteByte('.')
	version.WriteString(v.core[1])
	version.WriteByte('.')
	version.WriteString(v.core[2])
	if v.isPrerelease() {
		version.WriteByte('-')
		version.WriteString(strings.Join(v.prerelease, "."))
	}
	if len(v.build) > 0 {
		version.WriteByte('+')
		version.WriteString(strings.Join(v.build, "."))
	}
	return version.String()
}

func (v semanticVersion) isPrerelease() bool {
	return len(v.prerelease) > 0
}
