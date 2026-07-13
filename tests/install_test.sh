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

for file in "$root_dir/install.sh" "$root_dir/README.md"; do
	if grep -Eq 'raw\.githubusercontent\.com/.*/master/install\.sh|/master/install\.sh' "$file"; then
		printf 'mutable master install URL remains in %s\n' "$file" >&2
		exit 1
	fi
done

install_release_url='https://github.com/${OWNER}/${REPO}/releases/latest/download/install.sh'
install_release_url_count="$(grep -Fc "$install_release_url" "$root_dir/install.sh" || true)"
if [ "$install_release_url_count" -ne 2 ]; then
	printf 'install.sh usage must contain the release install URL twice\n' >&2
	exit 1
fi

readme_release_url='https://github.com/we11adam/uddns/releases/latest/download/install.sh'
readme_release_url_count="$(grep -Fc "$readme_release_url" "$root_dir/README.md" || true)"
if [ "$readme_release_url_count" -lt 2 ]; then
	printf 'README must contain the release install URL examples\n' >&2
	exit 1
fi

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

expect_path_rejected() {
	field="$1"
	value="$2"
	if (
		safe_inputs
		set_input "$field" "$value"
		validate_systemd_inputs
	) >/dev/null 2>&1; then
		printf 'expected %s to reject non-absolute path: %s\n' "$field" "$value" >&2
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

for field in INSTALL_DIR CONFIG_FILE LOG_DIR; do
	for path in 'relative/path' '.' './relative' '..' '../relative'; do
		expect_path_rejected "$field" "$path"
	done
done

safe_inputs
INSTALL_DIR='/opt/UD DNS/bin'
CONFIG_FILE='/etc/UD DNS/config file.yaml'
LOG_DIR='/var/log/UD DNS'
validate_systemd_inputs
LOG_DIR=''
validate_systemd_inputs

run_as_root_test_dir="$test_dir/run-as-root"
mkdir -p "$run_as_root_test_dir"

(
	calls="$run_as_root_test_dir/non-root-success"
	id() {
		printf '1000\n'
	}
	fake_command() {
		printf 'command:%s\n' "$*" >>"$calls"
	}
	sudo() {
		printf 'sudo:%s\n' "$*" >>"$calls"
		"$@"
	}

	run_as_root fake_command success
	expected="$(printf '%s\n' 'sudo:fake_command success' 'command:success')"
	actual="$(cat "$calls")"
	if [ "$actual" != "$expected" ]; then
		printf 'non-root command was not executed exactly once through sudo:\n%s\n' "$actual" >&2
		exit 1
	fi
)

(
	calls="$run_as_root_test_dir/non-root-failure"
	id() {
		printf '1000\n'
	}
	fake_command() {
		printf 'command:%s\n' "$*" >>"$calls"
		return 42
	}
	sudo() {
		printf 'sudo:%s\n' "$*" >>"$calls"
		"$@"
	}

	status=0
	run_as_root fake_command failure || status="$?"
	if [ "$status" -ne 42 ]; then
		printf 'run_as_root swallowed command failure status: %s\n' "$status" >&2
		exit 1
	fi
	expected="$(printf '%s\n' 'sudo:fake_command failure' 'command:failure')"
	actual="$(cat "$calls")"
	if [ "$actual" != "$expected" ]; then
		printf 'failed non-root command was executed more than once:\n%s\n' "$actual" >&2
		exit 1
	fi
)

(
	calls="$run_as_root_test_dir/root-success"
	id() {
		printf '0\n'
	}
	fake_command() {
		printf 'command:%s\n' "$*" >>"$calls"
	}
	sudo() {
		printf 'sudo:%s\n' "$*" >>"$calls"
		return 1
	}

	run_as_root fake_command success
	actual="$(cat "$calls")"
	if [ "$actual" != 'command:success' ]; then
		printf 'root command was not executed exactly once directly:\n%s\n' "$actual" >&2
		exit 1
	fi
)

(
	calls="$run_as_root_test_dir/no-sudo"
	no_sudo_path="$run_as_root_test_dir/empty-path"
	mkdir -p "$no_sudo_path"
	PATH="$no_sudo_path"
	id() {
		printf '1000\n'
	}
	fake_command() {
		printf 'command:%s\n' "$*" >>"$calls"
	}

	if (run_as_root fake_command unavailable) >/dev/null 2>&1; then
		printf 'non-root command without sudo unexpectedly succeeded\n' >&2
		exit 1
	fi
	if [ -e "$calls" ]; then
		printf 'non-root command ran without sudo\n' >&2
		exit 1
	fi
)

config_test_dir="$test_dir/config-availability"
missing_config="$config_test_dir/missing.yaml"
directory_config="$config_test_dir/directory.yaml"
fifo_config="$config_test_dir/fifo.yaml"
readable_config="$config_test_dir/readable.yaml"
unreadable_config="$config_test_dir/unreadable.yaml"
mkdir -p "$directory_config"
mkfifo "$fifo_config"
printf 'jobs: []\n' >"$readable_config"
printf 'jobs: []\n' >"$unreadable_config"
chmod 000 "$unreadable_config"

(
	id() {
		[ "$1" = "-u" ] || return 1
		printf '1000\n'
	}
	sudo() {
		[ "$1" = "-n" ] || return 1
		shift
		[ "$1" = "test" ] || return 1
		shift
		predicate="$1"
		path="$2"
		if [ "$path" = "$unreadable_config" ] && [ "$predicate" = "-r" ]; then
			return 1
		fi
		test "$predicate" "$path"
	}

	if config_file_available_to_service "$missing_config"; then
		printf 'missing config was considered available\n' >&2
		exit 1
	fi
	if config_file_available_to_service "$directory_config"; then
		printf 'directory config was considered available\n' >&2
		exit 1
	fi
	if config_file_available_to_service "$fifo_config"; then
		printf 'FIFO config was considered available\n' >&2
		exit 1
	fi
	if ! config_file_available_to_service "$readable_config"; then
		printf 'readable regular config was considered unavailable\n' >&2
		exit 1
	fi
	if config_file_available_to_service "$unreadable_config"; then
		printf 'unreadable config was considered available\n' >&2
		exit 1
	fi
)
chmod 0600 "$unreadable_config"

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

expect_tar_rejected() {
	archive="$1"
	description="$2"
	rejected_extract_dir="${archive}.extract"
	mkdir -p "$rejected_extract_dir"
	if (extract_tar_archive "$archive" "$rejected_extract_dir") >/dev/null 2>&1; then
		printf 'expected tar archive to be rejected: %s\n' "$description" >&2
		exit 1
	fi
	extracted_count="$(find "$rejected_extract_dir" -print | wc -l | tr -d '[:space:]')"
	if [ "$extracted_count" != "1" ]; then
		printf 'rejected tar archive extracted files before validation completed: %s\n' "$description" >&2
		exit 1
	fi
}

REPO="uddns"
tar_test_dir="$test_dir/tar-archives"
mkdir -p "$tar_test_dir/legal-source" "$tar_test_dir/legal-extract"
printf '#!/bin/sh\n' >"$tar_test_dir/legal-source/uddns"
tar -czf "$tar_test_dir/legal.tar.gz" -C "$tar_test_dir/legal-source" uddns
extract_tar_archive "$tar_test_dir/legal.tar.gz" "$tar_test_dir/legal-extract"
if ! cmp "$tar_test_dir/legal-source/uddns" "$tar_test_dir/legal-extract/uddns" >/dev/null 2>&1; then
	printf 'legal uddns tar archive was not extracted correctly\n' >&2
	exit 1
fi

mkdir -p "$tar_test_dir/absolute-source"
printf 'absolute\n' >"$tar_test_dir/absolute-source/uddns"
tar -czPf "$tar_test_dir/absolute.tar.gz" "$tar_test_dir/absolute-source/uddns"
expect_tar_rejected "$tar_test_dir/absolute.tar.gz" "absolute member path"

mkdir -p "$tar_test_dir/traversal-source"
printf 'traversal\n' >"$tar_test_dir/traversal-source/uddns"
if tar --version 2>&1 | grep -qi 'bsdtar'; then
	tar -czf "$tar_test_dir/traversal.tar.gz" -s ',^uddns$,../uddns,' -C "$tar_test_dir/traversal-source" uddns
else
	tar -czf "$tar_test_dir/traversal.tar.gz" --transform='s|^uddns$|../uddns|' -C "$tar_test_dir/traversal-source" uddns
fi
expect_tar_rejected "$tar_test_dir/traversal.tar.gz" "parent path traversal"

mkdir -p "$tar_test_dir/symlink-source"
ln -s ../../outside "$tar_test_dir/symlink-source/uddns"
tar -czf "$tar_test_dir/symlink.tar.gz" -C "$tar_test_dir/symlink-source" uddns
expect_tar_rejected "$tar_test_dir/symlink.tar.gz" "symlink target escape"

mkdir -p "$tar_test_dir/hardlink-source"
printf 'hardlink\n' >"$tar_test_dir/hardlink-source/target"
ln "$tar_test_dir/hardlink-source/target" "$tar_test_dir/hardlink-source/uddns"
tar -cf "$tar_test_dir/hardlink-with-target.tar" -C "$tar_test_dir/hardlink-source" target uddns
dd if="$tar_test_dir/hardlink-with-target.tar" of="$tar_test_dir/hardlink.tar" bs=512 skip=2 2>/dev/null
gzip -c "$tar_test_dir/hardlink.tar" >"$tar_test_dir/hardlink.tar.gz"
hardlink_type="$(LC_ALL=C tar -tvzf "$tar_test_dir/hardlink.tar.gz" | cut -c 1)"
if [ "$hardlink_type" != "h" ]; then
	printf 'failed to create hardlink tar test fixture\n' >&2
	exit 1
fi
expect_tar_rejected "$tar_test_dir/hardlink.tar.gz" "hardlink member"

mkdir -p "$tar_test_dir/hierarchy-link-source" "$tar_test_dir/hierarchy-file-source/dir"
ln -s .. "$tar_test_dir/hierarchy-link-source/dir"
printf 'escape\n' >"$tar_test_dir/hierarchy-file-source/dir/escape"
tar -cf "$tar_test_dir/hierarchy.tar" -C "$tar_test_dir/hierarchy-link-source" dir
tar -rf "$tar_test_dir/hierarchy.tar" -C "$tar_test_dir/hierarchy-file-source" dir/escape
gzip -c "$tar_test_dir/hierarchy.tar" >"$tar_test_dir/hierarchy.tar.gz"
expect_tar_rejected "$tar_test_dir/hierarchy.tar.gz" "member nested below archive symlink"

expect_zip_rejected() {
	archive="$1"
	description="$2"
	rejected_extract_dir="${archive}.extract"
	mkdir -p "$rejected_extract_dir"
	if (extract_zip_archive "$archive" "$rejected_extract_dir") >/dev/null 2>&1; then
		printf 'expected zip archive to be rejected: %s\n' "$description" >&2
		exit 1
	fi
	extracted_count="$(find "$rejected_extract_dir" -print | wc -l | tr -d '[:space:]')"
	if [ "$extracted_count" != "1" ]; then
		printf 'rejected zip archive extracted files before validation completed: %s\n' "$description" >&2
		exit 1
	fi
}

zip_test_dir="$test_dir/zip-archives"
mkdir -p "$zip_test_dir/legal-source" "$zip_test_dir/legal-extract" "$zip_test_dir/exe-extract"
printf '#!/bin/sh\n' >"$zip_test_dir/legal-source/uddns"
printf 'windows binary\n' >"$zip_test_dir/legal-source/uddns.exe"
zip -q -j "$zip_test_dir/legal.zip" "$zip_test_dir/legal-source/uddns"
extract_zip_archive "$zip_test_dir/legal.zip" "$zip_test_dir/legal-extract"
if ! cmp "$zip_test_dir/legal-source/uddns" "$zip_test_dir/legal-extract/uddns" >/dev/null 2>&1; then
	printf 'legal uddns zip archive was not extracted correctly\n' >&2
	exit 1
fi
zip -q -j "$zip_test_dir/legal-exe.zip" "$zip_test_dir/legal-source/uddns.exe"
extract_zip_archive "$zip_test_dir/legal-exe.zip" "$zip_test_dir/exe-extract"
if ! cmp "$zip_test_dir/legal-source/uddns.exe" "$zip_test_dir/exe-extract/uddns.exe" >/dev/null 2>&1; then
	printf 'legal uddns.exe zip archive was not extracted correctly\n' >&2
	exit 1
fi

mkdir -p "$zip_test_dir/traversal-work"
printf 'traversal\n' >"$zip_test_dir/uddns"
(
	cd "$zip_test_dir/traversal-work"
	zip -q "$zip_test_dir/traversal.zip" ../uddns
)
expect_zip_rejected "$zip_test_dir/traversal.zip" "parent path traversal"

LC_ALL=C sed 's|\.\./uddns|/./uddns|g' "$zip_test_dir/traversal.zip" >"$zip_test_dir/absolute.zip"
if ! unzip -tqq "$zip_test_dir/absolute.zip" >/dev/null 2>&1; then
	printf 'failed to create valid absolute-path zip test fixture\n' >&2
	exit 1
fi
absolute_member="$(unzip -Z1 "$zip_test_dir/absolute.zip")"
if [ "$absolute_member" != "/./uddns" ]; then
	printf 'absolute-path zip fixture has unexpected member: %s\n' "$absolute_member" >&2
	exit 1
fi
expect_zip_rejected "$zip_test_dir/absolute.zip" "absolute member path"

mkdir -p "$zip_test_dir/directory-source/dir"
(
	cd "$zip_test_dir/directory-source"
	zip -q -r "$zip_test_dir/directory.zip" dir
)
expect_zip_rejected "$zip_test_dir/directory.zip" "directory member"

printf 'extra\n' >"$zip_test_dir/legal-source/extra"
zip -q -j "$zip_test_dir/multiple.zip" "$zip_test_dir/legal-source/uddns" "$zip_test_dir/legal-source/extra"
expect_zip_rejected "$zip_test_dir/multiple.zip" "multiple members"

mkdir -p "$zip_test_dir/symlink-source"
ln -s ../../outside "$zip_test_dir/symlink-source/uddns"
zip -q -y -j "$zip_test_dir/symlink.zip" "$zip_test_dir/symlink-source/uddns"
expect_zip_rejected "$zip_test_dir/symlink.zip" "symlink member"

mkdir -p "$zip_test_dir/fifo-source"
mkfifo "$zip_test_dir/fifo-source/uddns"
(printf 'fifo' >"$zip_test_dir/fifo-source/uddns") &
fifo_writer_pid="$!"
zip -q -FI -j "$zip_test_dir/fifo.zip" "$zip_test_dir/fifo-source/uddns"
wait "$fifo_writer_pid"
fifo_type="$(LC_ALL=C unzip -Z -l "$zip_test_dir/fifo.zip" | sed -n '/^[?bcdhlps-]/p' | cut -c 1)"
if [ "$fifo_type" != "p" ]; then
	printf 'failed to create FIFO zip test fixture\n' >&2
	exit 1
fi
expect_zip_rejected "$zip_test_dir/fifo.zip" "FIFO member"

(
	unzip() {
		return 1
	}
	if (validate_zip_archive "$zip_test_dir/legal.zip") >/dev/null 2>&1; then
		printf 'zip validation succeeded without reliable type inspection\n' >&2
		exit 1
	fi
)

printf 'install.sh input validation tests passed\n'
