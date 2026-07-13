#!/bin/sh
set -eu

minimum_coverage=70

check_summary() {
	minimum="$1"
	case "$minimum" in
		'' | *[!0-9]*)
			printf 'coverage threshold must be an integer: %s\n' "$minimum" >&2
			return 2
			;;
	esac
	if [ "$minimum" -gt 100 ]; then
		printf 'coverage threshold must not exceed 100: %s\n' "$minimum" >&2
		return 2
	fi

	awk -v minimum="$minimum" '
		$1 == "total:" {
			found = 1
			raw = $NF
			valid = raw ~ /%$/
			sub(/%$/, "", raw)
			valid = valid && raw ~ /^[0-9]+([.][0-9]+)?$/
			if (valid) {
				split(raw, parts, ".")
				coverage = parts[1] + 0
			}
		}
		END {
			if (!found) {
				print "coverage summary does not contain a total line"
				exit 2
			}
			if (!valid || coverage < 0 || coverage > 100) {
				print "coverage summary contains an invalid total percentage"
				exit 2
			}
			printf "statement coverage: %s%% (integer floor %d%%; minimum %d%%)\n", raw, coverage, minimum
			if (coverage < minimum) {
				exit 1
			}
		}
	'
}

if [ "${1:-}" = "--summary" ]; then
	[ "$#" -eq 2 ] || {
		printf 'usage: %s --summary <integer-minimum>\n' "$0" >&2
		exit 2
	}
	check_summary "$2"
	exit
fi

[ "$#" -eq 0 ] || {
	printf 'usage: %s [--summary <integer-minimum>]\n' "$0" >&2
	exit 2
}

root_dir="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
profile="$(mktemp 2>/dev/null || mktemp -t uddns-coverage)"
trap 'rm -f "$profile"' EXIT INT TERM

cd "$root_dir"
go test -coverprofile="$profile" ./...
go tool cover -func="$profile" | check_summary "$minimum_coverage"
