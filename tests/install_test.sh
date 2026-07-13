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

printf 'install.sh input validation tests passed\n'
