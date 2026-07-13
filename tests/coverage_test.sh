#!/bin/sh
set -eu

root_dir="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
checker="$root_dir/scripts/check-coverage.sh"

summary='github.com/example/no_tests.go:1:        NoTests             0.0%
github.com/example/generated.pb.go:10:     Generated           0.0%
total:                                    (statements)          73.5%'

output="$(printf '%s\n' "$summary" | sh "$checker" --summary 73)"
case "$output" in
	*'statement coverage: 73.5%'*) ;;
	*)
		printf 'unexpected coverage output: %s\n' "$output" >&2
		exit 1
		;;
esac

if printf '%s\n' "$summary" | sh "$checker" --summary 74 >/dev/null 2>&1; then
	printf 'expected coverage below the threshold to fail\n' >&2
	exit 1
fi

if printf '%s\n' 'total: (statements) invalid%' | sh "$checker" --summary 70 >/dev/null 2>&1; then
	printf 'expected malformed coverage to fail\n' >&2
	exit 1
fi

if printf '%s\n' 'github.com/example/generated.pb.go:10: Generated 0.0%' | sh "$checker" --summary 70 >/dev/null 2>&1; then
	printf 'expected a missing total line to fail\n' >&2
	exit 1
fi

if printf '%s\n' "$summary" | sh "$checker" --summary 70.5 >/dev/null 2>&1; then
	printf 'expected a non-integer threshold to fail\n' >&2
	exit 1
fi

if printf '%s\n' "$summary" | sh "$checker" --summary 101 >/dev/null 2>&1; then
	printf 'expected a threshold above 100 to fail\n' >&2
	exit 1
fi

printf 'coverage checker tests passed\n'
