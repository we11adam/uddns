# Changelog

All notable changes to UDDNS are documented here, based on the Git commit history.

## Unreleased

No changes yet.

## v1.4.0 - 2026-05-21

### Added

- Added configurable logging with `logging.level`, `logging.dir`, and `logging.retention_days` in `uddns.yaml`.
- Added matching environment overrides: `UDDNS_LOG_LEVEL`, `UDDNS_LOG_DIR`, and `UDDNS_LOG_RETENTION_DAYS`.
- Added daily calendar-based file log rotation using `uddns-YYYY-MM-DD.log` files.
- Added log retention cleanup for logs older than the configured number of calendar days.
- Added focused tests for scheduler behavior, log configuration, and log rotation.
- Added `CHANGELOG.md`.
- Added Chinese documentation in `README.zh-CN.md`.

### Changed

- Improved application logs with structured provider, updater, notifier, IP-change, update, and notification context.
- Reworked scheduler execution into a testable single-cycle flow.
- Updated the installer so systemd logging environment variables are optional and do not override config-file logging by default.
- Reorganized the English README around the current installation, configuration, logging, and release-history workflow.

## v1.3.1 - 2026-05-21

### Added

- Added a curl-based installer script.
- Added optional systemd service installation from the installer.
- Added Linux and Darwin build targets for amd64 and arm64 in the Makefile.

### Changed

- Simplified Cloudflare proxy conditionals.
- Documented the curl installer and systemd installation path.

## v1.3.0 - 2024-12-26

### Added

- Added proxy support for the Cloudflare updater.

## v1.2.1 - 2024-09-29

### Added

- Added notifier messages for DNS update failures.

## v1.2.0 - 2024-07-29

### Added

- Added Aliyun DNS updater support.

## v1.1.0 - 2024-07-15

### Added

- Added IPv6 support.
- Added LightDNS updater support.

### Fixed

- Clear cached Cloudflare zone and record IDs after update failures so later attempts can recover.
- Fixed provider construction to return concrete values where needed.
- Fixed minor typos and README details.

## v1.0.4 - 2024-06-13

### Added

- Added `ip.fm` as an external IP service provider.

## v1.0.3 - 2024-06-13

### Added

- Added Cloudflare API token authentication support.

## v1.0.2 - 2024-06-13

### Added

- Enforced IPv4 usage in the HTTP client for IPv4 IP-service lookups.

### Changed

- Reorganized imports in `main.go`.
- Merged external contribution from pull request #1.

## v1.0.1 - 2024-05-20

### Fixed

- Fixed the release page URL in README.
- Disabled colorized logging when output is not a terminal.

## v1.0.0 - 2024-05-17

### Added

- Added the initial GitHub Actions and GoReleaser release workflow.
- Added README documentation for installation, configuration, running, supported providers, updaters, and notifiers.
- Added configurable update interval via `UDDNS_INTERVAL`.
- Added multiple config file lookup locations, including local, user config, and `/etc`.
- Added provider support for external IP services and network interfaces.
- Added updater support for DuckDNS.
- Added notifier infrastructure and Telegram notifier support.
- Added an application layer for the main update loop.
- Added a simple Makefile.

### Changed

- Lowered the Go version requirement.
- Refactored constructors and application organization.
- Overhauled the initial logging setup.

### Fixed

- Fixed config path resolution from the home directory.
- Fixed config file lookup ordering.
- Added validation for required RouterOS and DuckDNS settings.
- Fixed slog key usage and minor typos.

## Before v1.0.0

### Added

- Initial project scaffold for UDDNS.
