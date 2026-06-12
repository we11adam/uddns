#!/bin/sh
set -eu

OWNER="${OWNER:-we11adam}"
REPO="${REPO:-uddns}"
VERSION="${UDDNS_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
SERVICE_NAME="${SERVICE_NAME:-uddns}"
CONFIG_FILE_SET=0
if [ -n "${UDDNS_CONFIG:-}" ]; then
	CONFIG_FILE="$UDDNS_CONFIG"
	CONFIG_FILE_SET=1
else
	CONFIG_FILE="/etc/uddns.yaml"
fi
SERVICE_INTERVAL="${UDDNS_INTERVAL:-30s}"
LOG_DIR="${UDDNS_LOG_DIR:-}"
LOG_RETENTION_DAYS="${UDDNS_LOG_RETENTION_DAYS:-}"
INSTALL_SYSTEMD="${UDDNS_INSTALL_SYSTEMD:-}"

usage() {
	cat <<EOF
Install UDDNS from GitHub releases.

Usage:
  curl -fsSL https://raw.githubusercontent.com/${OWNER}/${REPO}/master/install.sh | sh
  curl -fsSL https://raw.githubusercontent.com/${OWNER}/${REPO}/master/install.sh | sh -s -- [options]

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

path_parent_dir() {
	path="$1"
	case "$path" in
		*/*)
			parent="${path%/*}"
			[ -n "$parent" ] || parent="/"
			;;
		*)
			parent="."
			;;
	esac
	printf '%s\n' "$parent"
}

can_write_path() {
	path="$1"
	if [ -e "$path" ]; then
		[ -w "$path" ]
		return
	fi

	parent="$(path_parent_dir "$path")"
	while [ ! -d "$parent" ] && [ "$parent" != "/" ] && [ "$parent" != "." ]; do
		parent="$(path_parent_dir "$parent")"
	done

	[ -d "$parent" ] && [ -w "$parent" ]
}

config_file_available_to_service() {
	config_file="$1"

	[ -r "$config_file" ] && return 0
	[ -e "$config_file" ] && return 0
	return 1
}

warn_config_write_permission() {
	config_file="$1"
	needs_write="${2:-1}"

	[ "$(id -u)" -eq 0 ] && return
	[ "$needs_write" -eq 0 ] && return
	can_write_path "$config_file" && return

	if command -v sudo >/dev/null 2>&1; then
		log "Current user cannot write ${config_file}."
		log "Use sudo when creating or editing this config file, for example:"
		log "  sudo install -m 0600 /path/to/uddns.yaml ${config_file}"
	else
		log "Current user cannot write ${config_file}. Create or edit it as root before starting the service."
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

systemd_unit_path() {
	unit="${SERVICE_NAME}.service"

	for path in "/etc/systemd/system/${unit}" "/usr/lib/systemd/system/${unit}" "/lib/systemd/system/${unit}"; do
		if [ -e "$path" ]; then
			printf '%s\n' "$path"
			return 0
		fi
	done

	return 1
}

systemd_unit_exists() {
	unit="${SERVICE_NAME}.service"

	systemd_unit_path >/dev/null && return 0
	systemctl list-unit-files "$unit" --no-legend 2>/dev/null | grep -q "^${unit}[[:space:]]"
}

systemd_unit_config_file() {
	unit_path="$(systemd_unit_path)" || return 1

	sed -n \
		-e 's/^Environment=UDDNS_CONFIG=\(.*\)$/\1/p' \
		-e 's/^Environment="UDDNS_CONFIG=\(.*\)"$/\1/p' \
		"$unit_path" |
		head -n 1 |
		sed 's/\\"/"/g; s/\\\\/\\/g; s/%%/%/g'
}

systemd_quote() {
	value="$1"

	printf '"%s"' "$(printf '%s' "$value" | sed 's/\\/\\\\/g; s/"/\\"/g; s/%/%%/g')"
}

systemd_env_line() {
	name="$1"
	value="$2"

	printf 'Environment=%s\n' "$(systemd_quote "${name}=${value}")"
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
				CONFIG_FILE_SET=1
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
		aarch64 | arm64) printf 'arm64\n' ;;
		armv7*) printf 'armv7\n' ;;
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
		arm64) printf 'arm64|aarch64' ;;
		armv7) printf 'armv7|arm_7|arm-7|arm7' ;;
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

	printf '%s\n' "$candidates" | head -n 1
}

find_checksum_url() {
	release_json="$1"

	extract_urls "$release_json" | grep -E '/checksums\.txt$|/checksums\.txt\?' | head -n 1
}

sha256_file() {
	file="$1"

	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$file" | sed 's/[[:space:]].*//'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$file" | sed 's/[[:space:]].*//'
	elif command -v openssl >/dev/null 2>&1; then
		openssl dgst -sha256 "$file" | sed 's/^.*= //'
	else
		fail "required command not found: sha256sum, shasum, or openssl"
	fi
}

verify_checksum() {
	archive="$1"
	checksums="$2"
	asset_url="$3"
	asset_name="${asset_url##*/}"

	expected="$(
		while read -r checksum filename _; do
			if [ "$filename" = "$asset_name" ]; then
				printf '%s\n' "$checksum"
				break
			fi
		done <"$checksums"
	)"
	[ -n "$expected" ] || fail "no checksum found for ${asset_name}"

	actual="$(sha256_file "$archive")"
	[ "$actual" = "$expected" ] || fail "checksum mismatch for ${asset_name}"

	log "Verified checksum for ${asset_name}"
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

	checksum_url="$(find_checksum_url "$release_json")"
	[ -n "$checksum_url" ] || fail "no checksums.txt asset found in ${resolved_version}"
	checksums="${tmpdir}/checksums.txt"
	log "Downloading checksums for ${resolved_version}"
	curl -fsSL "$checksum_url" -o "$checksums"
	verify_checksum "$archive" "$checksums" "$asset_url"

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

installed_binary_path() {
	printf '%s/%s\n' "$INSTALL_DIR" "$REPO"
}

installed_binary_exists() {
	[ -e "$(installed_binary_path)" ]
}

upgrade_install_exists() {
	installed_binary_exists && return 0
	systemd_unit_exists
}

install_binary() {
	binary="$1"
	target="$(installed_binary_path)"

	if [ -e "$target" ]; then
		log "Upgrading ${REPO} at ${target}"
	else
		log "Installing ${REPO} to ${target}"
	fi
	run_as_root mkdir -p "$INSTALL_DIR"
	run_as_root install -m 0755 "$binary" "$target"
}

install_systemd_service() {
	binary_path="${INSTALL_DIR}/${REPO}"
	unit_path="/etc/systemd/system/${SERVICE_NAME}.service"
	unit_file="${tmpdir}/${SERVICE_NAME}.service"
	update_service=0
	needs_config_write=1

	if systemd_unit_exists; then
		update_service=1
	fi

	if [ "$update_service" -eq 1 ] && [ "$CONFIG_FILE_SET" -eq 0 ]; then
		existing_config="$(systemd_unit_config_file || true)"
		if [ -n "$existing_config" ]; then
			CONFIG_FILE="$existing_config"
			log "Reusing existing systemd config path: ${CONFIG_FILE}"
			if [ -e "$CONFIG_FILE" ]; then
				needs_config_write=0
			fi
		fi
	fi

	if [ "$update_service" -eq 0 ] && [ "$CONFIG_FILE" = "/etc/uddns.yaml" ] && is_tty_available; then
		CONFIG_FILE="$(prompt_text "Config file path for the systemd service" "$CONFIG_FILE")"
	fi
	warn_config_write_permission "$CONFIG_FILE" "$needs_config_write"

	cat >"$unit_file" <<EOF
[Unit]
Description=UDDNS dynamic DNS updater
Documentation=https://github.com/${OWNER}/${REPO}
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
EOF

	systemd_env_line UDDNS_CONFIG "$CONFIG_FILE" >>"$unit_file"
	systemd_env_line UDDNS_INTERVAL "$SERVICE_INTERVAL" >>"$unit_file"

	if [ -n "$LOG_DIR" ]; then
		systemd_env_line UDDNS_LOG_DIR "$LOG_DIR" >>"$unit_file"
	fi
	if [ -n "$LOG_RETENTION_DAYS" ]; then
		systemd_env_line UDDNS_LOG_RETENTION_DAYS "$LOG_RETENTION_DAYS" >>"$unit_file"
	fi

	cat >>"$unit_file" <<EOF
ExecStart=$(systemd_quote "$binary_path")
Restart=on-failure
RestartSec=10s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
RestrictSUIDSGID=true
LockPersonality=true
SystemCallArchitectures=native
EOF

	if [ -n "$LOG_DIR" ]; then
		printf 'ReadWritePaths=%s\n' "$(systemd_quote "$LOG_DIR")" >>"$unit_file"
	fi

	cat >>"$unit_file" <<EOF

[Install]
WantedBy=multi-user.target
EOF

	if [ "$update_service" -eq 1 ]; then
		log "Updating systemd unit at ${unit_path}"
	else
		log "Installing systemd unit to ${unit_path}"
	fi
	run_as_root install -m 0644 "$unit_file" "$unit_path"
	run_as_root systemctl daemon-reload

	if config_file_available_to_service "$CONFIG_FILE"; then
		run_as_root systemctl enable --now "${SERVICE_NAME}.service"
		log "systemd service enabled and started: ${SERVICE_NAME}.service"
	else
		run_as_root systemctl enable "${SERVICE_NAME}.service"
		log "systemd service enabled but not started because ${CONFIG_FILE} is not readable"
		log "Create the config file, then run: sudo systemctl start ${SERVICE_NAME}.service"
	fi
}

parse_args "$@"

upgrade_install=0
if upgrade_install_exists; then
	upgrade_install=1
fi

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

if [ "$upgrade_install" -eq 1 ]; then
	log "UDDNS upgraded successfully."
else
	log "UDDNS installed successfully."
fi
