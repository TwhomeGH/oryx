#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 2 ]]; then
    echo "Usage: $0 <title> <exit-code> [log-file...]"
    exit 1
fi

title="$1"; shift
ret="$1"; shift

if [[ "$ret" == "0" ]]; then
    exit 0
fi

escape_annotation() {
    local v="${1//$'%'/%25}"
    v="${v//$'\r'/%0D}"
    v="${v//$'\n'/%0A}"
    echo "$v"
}

escape_property() {
    local v="${1//$'%'/%25}"
    v="${v//$'\r'/%0D}"
    v="${v//$'\n'/%0A}"
    v="${v//,/%2C}"
    v="${v//:/%3A}"
    echo "$v"
}

collect_matches() {
    local pattern="$1"; shift
    local limit="$1"; shift
    for log in "$@"; do
        [[ -f "$log" ]] || continue
        grep -aE "$pattern" "$log" || true
    done | sed -E 's/\x1b\[[0-9;]*m//g' | head -n "$limit" || true
}

emit_go_test_annotations() {
    local count=0
    local log line file lineno message

    for log in "$@"; do
        [[ -f "$log" ]] || continue
        while IFS= read -r line; do
            if [[ "$line" =~ ^[[:space:]]*([^[:space:]:]+_test\.go):([0-9]+):[[:space:]]*(.*)$ ]]; then
                file="${BASH_REMATCH[1]}"
                lineno="${BASH_REMATCH[2]}"
                message="${BASH_REMATCH[3]}"
                echo "::error file=$(escape_property "$file"),line=$lineno,title=$(escape_property "$title failed")::$(escape_annotation "$message")"
                count=$((count + 1))
                [[ $count -ge 10 ]] && return
            fi
        done < <(grep -aE '^[[:space:]]*[^[:space:]:]+_test\.go:[0-9]+:' "$log" || true)
    done
}

failed_tests="$(collect_matches '^--- FAIL: ' 20 "$@" | sed -E 's/^--- FAIL: ([^ ]+).*/\1/' | sort -u)"
go_locations="$(collect_matches '^[[:space:]]*[^[:space:]:]+_test\.go:[0-9]+:' 40 "$@")"
test_errors="$(collect_matches 'Fail for err|Prepare test fail|panic:|fatal error:|FAIL[[:space:]]|exit status|Error:|ERROR' 40 "$@")"
service_errors="$(collect_matches 'Start SRS failed|Start platform failed|Check SRS failed|conf error|panic|fatal|ERROR|failed' 40 "$@")"

summary="Exit code: $ret"
if [[ -n "$failed_tests" ]]; then
    summary="$summary

Failed tests:
$failed_tests"
fi
if [[ -n "$go_locations" ]]; then
    summary="$summary

Go test locations:
$go_locations"
fi
if [[ -n "$test_errors" ]]; then
    summary="$summary

Test errors:
$test_errors"
fi
if [[ -n "$service_errors" ]]; then
    summary="$summary

Service errors:
$service_errors"
fi

if [[ "$summary" == "Exit code: $ret" ]]; then
    summary="$summary

No structured failure lines were found. Check the full job log and attached service log."
fi

echo "::error title=$(escape_annotation "$title failed")::$(escape_annotation "$summary")"
emit_go_test_annotations "$@" || true

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
    {
        echo "## $title failed"
        echo
        echo "Exit code: \`$ret\`"
        echo
        if [[ -n "$failed_tests" ]]; then
            echo "### Failed tests"
            echo '```text'
            echo "$failed_tests"
            echo '```'
            echo
        fi
        if [[ -n "$go_locations" ]]; then
            echo "### Go test locations"
            echo '```text'
            echo "$go_locations"
            echo '```'
            echo
        fi
        if [[ -n "$test_errors" ]]; then
            echo "### Test errors"
            echo '```text'
            echo "$test_errors"
            echo '```'
            echo
        fi
        if [[ -n "$service_errors" ]]; then
            echo "### Service errors"
            echo '```text'
            echo "$service_errors"
            echo '```'
            echo
        fi
        echo "### Logs"
        for log in "$@"; do
            if [[ -f "$log" ]]; then
                echo "- \`$log\`"
            fi
        done
    } >> "$GITHUB_STEP_SUMMARY"
fi

exit 0
