package selfupdate

import (
	"fmt"
	"strings"
)

// assetName returns the archive name produced by the project's GoReleaser
// configuration for a supported runtime target. The version is the artifact
// version without the release tag's leading "v".
func assetName(project, version, goos, goarch string) (string, error) {
	if err := validateArtifactComponent("project", project); err != nil {
		return "", err
	}
	if err := validateArtifactComponent("version", version); err != nil {
		return "", err
	}
	if strings.HasPrefix(version, "v") {
		return "", fmt.Errorf("version must not include a leading v")
	}

	artifactArch := goarch
	extension := "zip"

	switch goos {
	case "linux":
		extension = "tar.gz"
		switch goarch {
		case "amd64", "arm64":
		case "arm":
			// linux_arm_7 is the only 32-bit ARM target in .goreleaser.yaml.
			artifactArch = "armv7"
		default:
			return "", unsupportedTargetError(goos, goarch)
		}
	case "darwin":
		if goarch != "amd64" && goarch != "arm64" {
			return "", unsupportedTargetError(goos, goarch)
		}
	case "freebsd":
		if goarch != "amd64" {
			return "", unsupportedTargetError(goos, goarch)
		}
	case "windows":
		if goarch != "amd64" && goarch != "arm64" {
			return "", unsupportedTargetError(goos, goarch)
		}
	default:
		return "", unsupportedTargetError(goos, goarch)
	}

	return fmt.Sprintf(
		"%s_%s_%s_%s.%s",
		project,
		version,
		goos,
		artifactArch,
		extension,
	), nil
}

func archiveExecutableName(project, goos string) string {
	if goos == "windows" {
		return project + ".exe"
	}
	return project
}

func validateArtifactComponent(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if value == "." ||
		value == ".." ||
		strings.ContainsAny(value, `/\`) ||
		strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s must not contain a path", name)
	}
	return nil
}

func unsupportedTargetError(goos, goarch string) error {
	return fmt.Errorf("unsupported release target %s/%s", goos, goarch)
}
