#!/bin/sh
set -eu

root_dir="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
test_dir="$(mktemp -d 2>/dev/null || mktemp -d -t uddns-install-test)"
trap 'rm -rf "$test_dir"' EXIT INT TERM

# Source the function definitions without running the installer.
if ! grep -q '^parse_args "\$@"$' "$root_dir/install.sh"; then
	printf 'could not locate install.sh main entry point\n' >&2
	exit 1
fi
sed '/^parse_args "\$@"$/,$d' "$root_dir/install.sh" >"$test_dir/install-functions.sh"
. "$test_dir/install-functions.sh"

safe_inputs() {
	SERVICE_NAME="uddns-test@blue_1"
	OWNER="example owner"
	REPO="uddns repo"
	INSTALL_DIR="/opt/UD DNS/bin"
	CONFIG_FILE='/etc/UD DNS/config "blue".yaml'
	SERVICE_INTERVAL="30s"
	LOG_DIR="/var/log/UD DNS"
	LOG_RETENTION_DAYS="7"
}

set_input() {
	case "$1" in
		OWNER) OWNER="$2" ;;
		REPO) REPO="$2" ;;
		INSTALL_DIR) INSTALL_DIR="$2" ;;
		CONFIG_FILE) CONFIG_FILE="$2" ;;
		SERVICE_INTERVAL) SERVICE_INTERVAL="$2" ;;
		LOG_DIR) LOG_DIR="$2" ;;
		LOG_RETENTION_DAYS) LOG_RETENTION_DAYS="$2" ;;
		*) printf 'unknown test input: %s\n' "$1" >&2; exit 1 ;;
	esac
}

expect_rejected() {
	field="$1"
	value="$2"
	if (
		safe_inputs
		set_input "$field" "$value"
		validate_systemd_inputs
	) >/dev/null 2>&1; then
		printf 'expected %s to reject line breaks\n' "$field" >&2
		exit 1
	fi
}

safe_inputs
validate_systemd_inputs

quoted_config="$(systemd_env_line UDDNS_CONFIG "$CONFIG_FILE")"
expected_config='Environment="UDDNS_CONFIG=/etc/UD DNS/config \"blue\".yaml"'
if [ "$quoted_config" != "$expected_config" ]; then
	printf 'unexpected quoted config: %s\n' "$quoted_config" >&2
	exit 1
fi

for service_name in '' '-evil' '../evil' 'evil/name' 'evil\name' 'evil name'; do
	if (
		safe_inputs
		SERVICE_NAME="$service_name"
		validate_systemd_inputs
	) >/dev/null 2>&1; then
		printf 'expected unsafe SERVICE_NAME to be rejected: %s\n' "$service_name" >&2
		exit 1
	fi
done

line_feed_value='safe
Injected=true'
carriage_return_value="$(printf 'safe\rInjected=true')"
for field in OWNER REPO INSTALL_DIR CONFIG_FILE SERVICE_INTERVAL LOG_DIR LOG_RETENTION_DAYS; do
	expect_rejected "$field" "$line_feed_value"
	expect_rejected "$field" "$carriage_return_value"
done

calls_file="$test_dir/run-as-root-calls"
unit_exists=0
systemd_unit_exists() {
	[ "$unit_exists" -eq 1 ]
}
is_tty_available() {
	return 1
}
warn_config_write_permission() {
	:
}
ensure_service_user() {
	:
}
config_file_available_to_service() {
	return 0
}
run_as_root() {
	printf '%s\n' "$*" >>"$calls_file"
}
assert_systemctl_calls() {
	expected="$1"
	actual="$(sed -n '/^systemctl /p' "$calls_file")"
	if [ "$actual" != "$expected" ]; then
		printf 'unexpected systemctl calls:\n%s\n' "$actual" >&2
		exit 1
	fi
}

safe_inputs
CONFIG_FILE_SET=1
tmpdir="$test_dir"

: >"$calls_file"
unit_exists=0
install_systemd_service >/dev/null 2>&1
assert_systemctl_calls "$(printf '%s\n' \
	'systemctl daemon-reload' \
	'systemctl enable --now uddns-test@blue_1.service')"

: >"$calls_file"
unit_exists=1
install_systemd_service >/dev/null 2>&1
assert_systemctl_calls "$(printf '%s\n' \
	'systemctl daemon-reload' \
	'systemctl enable uddns-test@blue_1.service' \
	'systemctl restart uddns-test@blue_1.service')"

unit_file="$test_dir/uddns-test@blue_1.service"
for expected_line in \
	'User=uddns' \
	'Group=uddns' \
	'LoadCredential="uddns.yaml:/etc/UD DNS/config \"blue\".yaml"' \
	'Environment="UDDNS_CONFIG=%d/uddns.yaml"' \
	'CapabilityBoundingSet=' \
	'RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6' \
	'LogsDirectory=uddns' \
	'UMask=0077'; do
	if ! grep -Fqx "$expected_line" "$unit_file"; then
		printf 'systemd unit missing expected line: %s\n' "$expected_line" >&2
		exit 1
	fi
done

download_release_body="$(sed -n '/^download_release() {/,/^}/p' "$root_dir/install.sh")"
download_calls="$(printf '%s\n' "$download_release_body" | grep -c '^[[:space:]]*download_file ' || true)"
if [ "$download_calls" -ne 3 ]; then
	printf 'expected all three release downloads to use download_file, got %s\n' "$download_calls" >&2
	exit 1
fi
if printf '%s\n' "$download_release_body" | grep -q '^[[:space:]]*curl '; then
	printf 'download_release contains a direct curl call\n' >&2
	exit 1
fi

fake_bin="$test_dir/fake-bin"
curl_calls="$test_dir/curl-calls"
mkdir -p "$fake_bin"
cat >"$fake_bin/curl" <<'EOF'
#!/bin/sh
printf '%s\n' "$@" >>"$CURL_CALLS_FILE"
EOF
chmod +x "$fake_bin/curl"
CURL_CALLS_FILE="$curl_calls"
export CURL_CALLS_FILE
PATH="$fake_bin:$PATH"
export PATH

download_url="https://downloads.example.test/uddns.tar.gz"
download_output="$test_dir/download-output"
download_file "$download_url" "$download_output"
expected_curl_calls="$(printf '%s\n' \
	'-fsSL' \
	'--proto' '=https' \
	'--proto-redir' '=https' \
	'--connect-timeout' '10' \
	'--max-time' '300' \
	'--retry' '3' \
	'--retry-delay' '1' \
	'--retry-max-time' '300' \
	"$download_url" '-o' "$download_output")"
actual_curl_calls="$(cat "$curl_calls")"
if [ "$actual_curl_calls" != "$expected_curl_calls" ]; then
	printf 'unexpected curl arguments:\n%s\n' "$actual_curl_calls" >&2
	exit 1
fi

printf 'install.sh input validation tests passed\n'
