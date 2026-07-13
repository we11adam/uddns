#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
	printf 'usage: %s <release-archive-directory>\n' "$0" >&2
	exit 2
fi

root_dir="$(CDPATH='' cd "$(dirname "$0")/.." && pwd)"
archive_dir="$1"
test_dir="$(mktemp -d 2>/dev/null || mktemp -d -t uddns-release-archive-test)"
trap 'rm -rf "$test_dir"' EXIT INT TERM

if [ ! -d "$archive_dir" ]; then
	printf 'release archive directory does not exist: %s\n' "$archive_dir" >&2
	exit 1
fi

# Source the installer functions without running its main entry point so the
# generated archives are checked against the installer's real acceptance rules.
if ! grep -q '^parse_args "\$@"$' "$root_dir/install.sh"; then
	printf 'could not locate install.sh main entry point\n' >&2
	exit 1
fi
sed '/^parse_args "\$@"$/,$d' "$root_dir/install.sh" >"$test_dir/install-functions.sh"
# shellcheck source=/dev/null
. "$test_dir/install-functions.sh"

REPO="uddns"
export REPO
archive_count=0
tar_count=0
zip_count=0

for archive in "$archive_dir"/*.tar.gz "$archive_dir"/*.tgz "$archive_dir"/*.zip; do
	[ -f "$archive" ] || continue
	archive_count=$((archive_count + 1))
	local_archive="$test_dir/archive-${archive_count}"
	cp "$archive" "$local_archive"

	case "$archive" in
		*.tar.gz | *.tgz)
			validate_tar_archive "$local_archive"
			tar_count=$((tar_count + 1))
			;;
		*.zip)
			validate_zip_archive "$local_archive"
			zip_count=$((zip_count + 1))
			;;
	esac
done

if [ "$archive_count" -eq 0 ]; then
	printf 'no release archives found in %s\n' "$archive_dir" >&2
	exit 1
fi
if [ "$tar_count" -eq 0 ] || [ "$zip_count" -eq 0 ]; then
	printf 'expected both tar.gz and zip release archives in %s\n' "$archive_dir" >&2
	exit 1
fi

printf 'validated %s release archives (%s tar.gz, %s zip)\n' \
	"$archive_count" "$tar_count" "$zip_count"
