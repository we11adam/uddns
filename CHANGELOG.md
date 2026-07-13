# Changelog

All notable changes to UDDNS are documented here, based on the Git commit history.

## Unreleased

No changes yet.

## v1.7.1 - 2026-07-13

### Fixed

- Fixed release archives to contain only the platform executable, restoring compatibility with the installer's strict single-file archive validation.

## v1.7.0 - 2026-07-13

### Added

- Added bounded retries for transient IP-service and DNS update failures, plus per-job exponential backoff with jitter.
- Added release SBOMs and GitHub Actions provenance attestations for release artifacts and `install.sh`.
- Expanded CI with race-enabled tests, a coverage floor, Staticcheck, `govulncheck`, installer tests, and ShellCheck; release actions and tools are pinned to fixed revisions or versions.

### Changed

- Raised the minimum Go version to 1.26.5, refreshed dependencies, and migrated Aliyun DNS support to the maintained Alibaba Cloud SDK.
- Hardened outbound HTTP with HTTPS for the bundled public-IP services and Aliyun, same-origin HTTPS redirect restrictions, strict proxy validation, timeouts and response-body limits, credential redaction, and context cancellation.
- Public IP services now reject non-public addresses; provider and verified addresses are normalized before comparison, and network-interface address selection is deterministic.
- Automatic DNS verification now queries only configured address families, is periodically throttled, and no longer blocks an IP update when verification is temporarily unavailable.
- The installer now downloads the release asset, validates tar/zip members before extraction, and uses HTTPS-only downloads with timeouts and retries while retaining checksum verification.
- systemd installations now run as a dedicated unprivileged `uddns` user, pass configuration through a systemd credential, and apply stricter input validation, sandboxing, and private permissions.
- Automatic `.env` loading has been removed. On Unix, configuration files must not be accessible to group or other users (use `chmod 600`), and `UDDNS_INTERVAL` is limited to 10 seconds–24 hours.

### Fixed

- Fixed DNS reconciliation so initially synchronized records are not rewritten, absent address families are preserved, and duplicate Cloudflare/Aliyun records are reconciled.
- Fixed DNS updater domain validation and rejected nil integration configurations before use.
- Fixed repeated IP-change and update-failure notifications and nil notifier handling.
- Fixed DuckDNS and Telegram success-response validation, response-body cleanup and limits, and preservation of public-IP service failure details.
- Fixed file logging permissions and rejected symlink or non-regular log targets.
- Fixed plugin registry races, ambiguous aliases, and nil constructor results.

## v1.6.1 - 2026-06-13

### Added

- Added `version` and `pid` fields to structured logs.
- Added release-build version injection through GoReleaser and Makefile builds.

### Changed

- Improved structured logging context for jobs, DNS records, providers, and updaters.
- Trimmed the release target matrix and installer architecture matching to the supported release assets.

## v1.6.0 - 2026-06-13

### Added

- Added advanced jobs mode for running multiple named DNS update jobs from one process.
- Added per-job provider, updater, DNS record, zone, and address-family selection.
- Added `verify` modes: `auto`, `off`, and `updater_api`.
- Added updater API verification for Cloudflare and Aliyun DNS records.
- Added `config check` to validate configuration without starting the scheduler.

### Changed

- Centralized config loading and per-job config overrides.
- Hardened the systemd installer and preserved existing service configuration during upgrades.
- Expanded CI validation before release.

### Fixed

- Fixed graceful shutdown handling.
- Fixed invalid notifier, IP, and DNS record configuration validation.

## v1.5.1 - 2026-06-03

### Fixed

- Warn when the selected config path requires sudo during installation.

## v1.5.0 - 2026-05-22

### Added

- Added `providers.use` and `updaters.use` for explicit provider/updater selection.
- Added Chinese changelog in `CHANGELOG.zh-CN.md`.

### Changed

- Replaced provider/updater map registries with a deterministic generic registry.
- Decoupled provider/updater constructors from direct Viper dependencies.
- Improved provider/updater configuration errors so missing configuration can be skipped while invalid configuration stops startup with context.
- Updated README links to point to both English and Chinese changelogs.

### Fixed

- Fixed LightDNS updater spelling and display-name casing while keeping the `updaters.lightdns` config key compatible.

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
