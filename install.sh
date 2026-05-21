#!/bin/sh
set -eu

OWNER="${OWNER:-we11adam}"
REPO="${REPO:-uddns}"
VERSION="${UDDNS_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
SERVICE_NAME="${SERVICE_NAME:-uddns}"
CONFIG_FILE="${UDDNS_CONFIG:-/etc/uddns.yaml}"
SERVICE_INTERVAL="${UDDNS_INTERVAL:-30s}"
LOG_DIR="${UDDNS_LOG_DIR:-}"
LOG_RETENTION_DAYS="${UDDNS_LOG_RETENTION_DAYS:-}"
INSTALL_SYSTEMD="${UDDNS_INSTALL_SYSTEMD:-}"

usage() {
	cat <<EOF
Install UDDNS from GitHub releases.

Usage:
  curl -fsSL https://raw.githubusercontent.com/${OWNER}/${REPO}/main/install.sh | sh
  curl -fsSL https://raw.githubusercontent.com/${OWNER}/${REPO}/main/install.sh | sh -s -- [options]

Options:
  --version <tag>       Release tag to install instead of latest
  --install-dir <dir>   Install directory (default: ${INSTALL_DIR})
  --systemd             Install or update the systemd service without prompting
  --no-systemd          Skip systemd service installation
  --config <path>       Config file path used by the systemd service
  --interval <duration> UDDNS_INTERVAL used by the systemd service
  --log-dir <dir>       Enable rotated file logging in the systemd service
  --log-retention-days <n>
                        Calendar days of logs to keep (application default: 7)
  -h, --help            Show this help

Environment:
  UDDNS_VERSION, INSTALL_DIR, UDDNS_INSTALL_SYSTEMD, UDDNS_CONFIG, UDDNS_INTERVAL,
  UDDNS_LOG_DIR, UDDNS_LOG_RETENTION_DAYS
EOF
}

log() {
	printf '%s\n' "$*" >&2
}

fail() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

run_as_root() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	elif "$@" 2>/dev/null; then
		return 0
	elif command -v sudo >/dev/null 2>&1; then
		sudo "$@"
	else
		fail "root permissions are required for: $*"
	fi
}

is_tty_available() {
	[ -r /dev/tty ] && [ -w /dev/tty ]
}

prompt_yes_no() {
	question="$1"
	default="${2:-n}"

	if ! is_tty_available; then
		[ "$default" = "y" ]
		return
	fi

	while :; do
		if [ "$default" = "y" ]; then
			suffix="[Y/n]"
		else
			suffix="[y/N]"
		fi

		printf '%s %s ' "$question" "$suffix" >/dev/tty
		IFS= read -r answer </dev/tty || answer=""
		answer="${answer:-$default}"

		case "$answer" in
			y | Y | yes | YES | Yes) return 0 ;;
			n | N | no | NO | No) return 1 ;;
			*) printf 'Please answer yes or no.\n' >/dev/tty ;;
		esac
	done
}

prompt_text() {
	question="$1"
	default="$2"

	if ! is_tty_available; then
		printf '%s\n' "$default"
		return
	fi

	printf '%s [%s] ' "$question" "$default" >/dev/tty
	IFS= read -r answer </dev/tty || answer=""
	printf '%s\n' "${answer:-$default}"
}

systemd_available() {
	command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]
}

systemd_unit_exists() {
	unit="${SERVICE_NAME}.service"

	[ -e "/etc/systemd/system/${unit}" ] && return 0
	[ -e "/usr/lib/systemd/system/${unit}" ] && return 0
	[ -e "/lib/systemd/system/${unit}" ] && return 0

	systemctl list-unit-files "$unit" --no-legend 2>/dev/null | grep -q "^${unit}[[:space:]]"
}

ask_systemd_preference() {
	case "$INSTALL_SYSTEMD" in
		1 | true | TRUE | yes | YES)
			systemd_available || fail "UDDNS_INSTALL_SYSTEMD is set, but systemd is not available"
			return 0
			;;
		0 | false | FALSE | no | NO)
			return 1
			;;
		"") ;;
		*)
			fail "invalid UDDNS_INSTALL_SYSTEMD value: $INSTALL_SYSTEMD"
			;;
	esac

	systemd_available || return 1

	if systemd_unit_exists; then
		prompt_yes_no "Existing ${SERVICE_NAME}.service detected. Update the systemd service?" "n"
	else
		prompt_yes_no "systemd detected. Install UDDNS as a systemd service?" "n"
	fi
}

parse_args() {
	while [ "$#" -gt 0 ]; do
		case "$1" in
			--version)
				[ "$#" -ge 2 ] || fail "--version requires a value"
				VERSION="$2"
				shift 2
				;;
			--install-dir)
				[ "$#" -ge 2 ] || fail "--install-dir requires a value"
				INSTALL_DIR="$2"
				shift 2
				;;
			--systemd)
				INSTALL_SYSTEMD=1
				shift
				;;
			--no-systemd)
				INSTALL_SYSTEMD=0
				shift
				;;
			--config)
				[ "$#" -ge 2 ] || fail "--config requires a value"
				CONFIG_FILE="$2"
				shift 2
				;;
			--interval)
				[ "$#" -ge 2 ] || fail "--interval requires a value"
				SERVICE_INTERVAL="$2"
				shift 2
				;;
			--log-dir)
				[ "$#" -ge 2 ] || fail "--log-dir requires a value"
				LOG_DIR="$2"
				shift 2
				;;
			--log-retention-days)
				[ "$#" -ge 2 ] || fail "--log-retention-days requires a value"
				LOG_RETENTION_DAYS="$2"
				shift 2
				;;
			-h | --help)
				usage
				exit 0
				;;
			*)
				fail "unknown option: $1"
				;;
		esac
	done
}

detect_os() {
	case "$(uname -s)" in
		Linux) printf 'linux\n' ;;
		Darwin) printf 'darwin\n' ;;
		FreeBSD) printf 'freebsd\n' ;;
		*) fail "unsupported operating system: $(uname -s)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64 | amd64) printf 'amd64\n' ;;
		i386 | i686) printf '386\n' ;;
		aarch64 | arm64) printf 'arm64\n' ;;
		armv5* | armv5tel) printf 'armv5\n' ;;
		armv6* | armv6l) printf 'armv6\n' ;;
		armv7* | armv7l) printf 'armv7\n' ;;
		mips64le) printf 'mips64le\n' ;;
		mips64) printf 'mips64\n' ;;
		mipsle) printf 'mipsle\n' ;;
		mips) printf 'mips\n' ;;
		s390x) printf 's390x\n' ;;
		riscv64) printf 'riscv64\n' ;;
		*) fail "unsupported architecture: $(uname -m)" ;;
	esac
}

os_regex() {
	case "$1" in
		linux) printf 'linux|Linux' ;;
		darwin) printf 'darwin|Darwin|macOS|MacOS' ;;
		freebsd) printf 'freebsd|FreeBSD' ;;
	esac
}

arch_regex() {
	case "$1" in
		amd64) printf 'amd64|x86_64' ;;
		386) printf '386|i386|i686' ;;
		arm64) printf 'arm64|aarch64' ;;
		armv5) printf 'armv5|arm_5|arm-5|arm5' ;;
		armv6) printf 'armv6|arm_6|arm-6|arm6' ;;
		armv7) printf 'armv7|arm_7|arm-7|arm7' ;;
		mips64le) printf 'mips64le' ;;
		mips64) printf 'mips64' ;;
		mipsle) printf 'mipsle' ;;
		mips) printf 'mips' ;;
		s390x) printf 's390x' ;;
		riscv64) printf 'riscv64' ;;
	esac
}

release_api_url() {
	if [ "$VERSION" = "latest" ]; then
		printf 'https://api.github.com/repos/%s/%s/releases/latest\n' "$OWNER" "$REPO"
	else
		printf 'https://api.github.com/repos/%s/%s/releases/tags/%s\n' "$OWNER" "$REPO" "$VERSION"
	fi
}

extract_urls() {
	sed -n 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1"
}

extract_tag() {
	sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | head -n 1
}

find_asset_url() {
	release_json="$1"
	os="$2"
	arch="$3"
	os_re="$(os_regex "$os")"
	arch_re="$(arch_regex "$arch")"

	candidates="$(extract_urls "$release_json" | grep -E '(\.tar\.gz|\.tgz|\.zip)$' | grep -E "(${os_re})" | grep -E "(${arch_re})" || true)"

	if [ "$arch" = "amd64" ]; then
		without_v3="$(printf '%s\n' "$candidates" | grep -Ev 'amd64[_-]?v3|x86_64[_-]?v3' || true)"
		if [ -n "$without_v3" ]; then
			candidates="$without_v3"
		fi
	fi

	printf '%s\n' "$candidates" | head -n 1
}

download_release() {
	tmpdir="$1"
	os="$2"
	arch="$3"

	release_json="${tmpdir}/release.json"
	api_url="$(release_api_url)"

	log "Fetching release metadata from ${api_url}"
	curl -fsSL "$api_url" -o "$release_json"

	resolved_version="$(extract_tag "$release_json")"
	[ -n "$resolved_version" ] || resolved_version="$VERSION"

	asset_url="$(find_asset_url "$release_json" "$os" "$arch")"
	[ -n "$asset_url" ] || fail "no release asset found for ${os}/${arch} in ${resolved_version}"

	archive="${tmpdir}/archive"
	log "Downloading ${REPO} ${resolved_version} for ${os}/${arch}"
	curl -fL "$asset_url" -o "$archive"

	extract_dir="${tmpdir}/extract"
	mkdir -p "$extract_dir"
	case "$asset_url" in
		*.tar.gz | *.tgz)
			need_cmd tar
			tar -xzf "$archive" -C "$extract_dir"
			;;
		*.zip)
			need_cmd unzip
			unzip -q "$archive" -d "$extract_dir"
			;;
		*)
			fail "unsupported archive type: $asset_url"
			;;
	esac

	binary="$(find "$extract_dir" -type f \( -name "$REPO" -o -name "${REPO}.exe" \) | head -n 1)"
	[ -n "$binary" ] || fail "archive did not contain a ${REPO} binary"

	printf '%s\n' "$binary"
}

install_binary() {
	binary="$1"
	target="${INSTALL_DIR}/${REPO}"

	log "Installing ${REPO} to ${target}"
	run_as_root mkdir -p "$INSTALL_DIR"
	run_as_root install -m 0755 "$binary" "$target"
}

install_systemd_service() {
	binary_path="${INSTALL_DIR}/${REPO}"
	unit_path="/etc/systemd/system/${SERVICE_NAME}.service"
	unit_file="${tmpdir}/${SERVICE_NAME}.service"

	if [ "$CONFIG_FILE" = "/etc/uddns.yaml" ] && is_tty_available; then
		CONFIG_FILE="$(prompt_text "Config file path for the systemd service" "$CONFIG_FILE")"
	fi

	cat >"$unit_file" <<EOF
[Unit]
Description=UDDNS dynamic DNS updater
Documentation=https://github.com/${OWNER}/${REPO}
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
Environment=UDDNS_CONFIG=${CONFIG_FILE}
Environment=UDDNS_INTERVAL=${SERVICE_INTERVAL}
EOF

	if [ -n "$LOG_DIR" ]; then
		printf 'Environment=UDDNS_LOG_DIR=%s\n' "$LOG_DIR" >>"$unit_file"
	fi
	if [ -n "$LOG_RETENTION_DAYS" ]; then
		printf 'Environment=UDDNS_LOG_RETENTION_DAYS=%s\n' "$LOG_RETENTION_DAYS" >>"$unit_file"
	fi

	cat >>"$unit_file" <<EOF
ExecStart=${binary_path}
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
EOF

	log "Installing systemd unit to ${unit_path}"
	run_as_root install -m 0644 "$unit_file" "$unit_path"
	run_as_root systemctl daemon-reload

	if [ -r "$CONFIG_FILE" ]; then
		run_as_root systemctl enable --now "${SERVICE_NAME}.service"
		log "systemd service enabled and started: ${SERVICE_NAME}.service"
	else
		run_as_root systemctl enable "${SERVICE_NAME}.service"
		log "systemd service enabled but not started because ${CONFIG_FILE} is not readable"
		log "Create the config file, then run: sudo systemctl start ${SERVICE_NAME}.service"
	fi
}

parse_args "$@"

want_systemd=0
if ask_systemd_preference; then
	want_systemd=1
fi

need_cmd curl
need_cmd uname
need_cmd grep
need_cmd sed
need_cmd find
need_cmd head
need_cmd install

tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t uddns)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

os="$(detect_os)"
arch="$(detect_arch)"
binary="$(download_release "$tmpdir" "$os" "$arch")"

install_binary "$binary"

if [ "$want_systemd" -eq 1 ]; then
	install_systemd_service
fi

log "UDDNS installed successfully."
