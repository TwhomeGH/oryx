#!/usr/bin/env bash
#
# Annotate Go unit-test failures from `go test -v` output and write a Job Summary, so a failure
# is not buried in hundreds of PASS lines. Non-fatal when no failures are found.
#
# Usage: bash scripts/tools/github-go-test-annotations.sh test.log
#
set -uo pipefail

log="${1:?usage: $0 <go-test-log>}"
summary="${GITHUB_STEP_SUMMARY:-/dev/null}"

if ! grep -q -- '--- FAIL' "$log" 2>/dev/null; then
	echo "No Go test failures." >> "$summary"
	exit 0
fi

{
	echo "## Go unit test failures"
	echo
	echo "| Test | Error |"
	echo "|---|---|"
} >> "$summary"

# Emit one ::error per failed test with the first indented message that follows it.
awk '
	/^--- FAIL: / { test=$3; inside=1; next }
	inside && /^[ \t]+/ {
		line=$0
		sub(/^[ \t]+/, "", line)
		print test "\t" line
		inside=0
	}
	/^=== RUN/ { inside=0 }
' "$log" | while IFS=$'\t' read -r test msg; do
	[ -n "${test:-}" ] || continue
	case "$test" in ---*|===*) continue;; esac
	echo "::error title=Go test failed: ${test}::${msg}"
	echo "| \`${test}\` | ${msg} |" >> "$summary"
done