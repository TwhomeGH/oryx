#!/usr/bin/env bash
#
# Turn the raw CI logs into GitHub annotations + a job summary that classify failures,
# so the real error can be judged from the check itself instead of digging through logs.
#
# Usage: github-test-annotations.sh <title> <exit-code> [log-file...]
#
# Classification:
#   1. Failed tests   - one annotation per "--- FAIL: TestX" with its concrete error line.
#   2. Root cause     - actionable environment/fixture hints (bad download, service start,
#                       invalid input, OOM, disk, ...).
#   3. Noise          - known benign lines are filtered and only counted, never reported.

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

logs=("$@")

strip_ansi() { sed -E 's/\x1b\[[0-9;]*m//g'; }

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

# Known-benign lines: never reported as a failure, only counted.
#   - SRS raw API probes without auth, retry banners, retry script chatter, docker warnings.
noise_re='no Authorization or token|/api/v1/summaries|/api/v1/versions|client disconnect peer\. ret=1007|Command failed\. Retrying|Running with retries=|Command failed after [0-9]+ retries|Memory overcommit must be enabled|stats disk not configed|disk iops disabled|is not stable|write log to console|srs hooks ignore program stream'

is_noise() { grep -qaE "$noise_re" <<<"$1"; }

# Actionable environment/fixture hints: "grep-pattern|human hint". The first matching
# non-noise line is reported, so a broken fixture is called out explicitly.
env_hints=(
    'Start SRS failed|SRS 服務啟動失敗'
    'Start platform failed|平台服務啟動失敗'
    'Check SRS failed|SRS 服務檢查失敗'
    'invalid data found when processing input|輸入媒體檔無效（常見於 CI 下載失敗／檔案損毀）'
    'could not find codec parameters|輸入媒體檔無法解析（檔案損毀）'
    'Cannot allocate memory|記憶體不足（容器被限制）'
    'no space left on device|磁碟空間不足'
    'Connection refused|服務未就緒或連線被拒'
    'killed: 9|程序被信號/OOM 終止'
    'Error response from daemon|Docker 容器啟動失敗'
    'failed to solve|映像建置失敗'
)

# 1) Failed tests: for each "--- FAIL: TestX" block, pick the first concrete error line.
failed_report=""
for log in "${logs[@]}"; do
    [[ -f "$log" ]] || continue
    # Go prints the test's error line(s) BETWEEN "=== RUN TestX" and "--- FAIL: TestX",
    # so buffer per test and emit on the FAIL marker.
    block="$(strip_ansi <"$log" | awk '
        function emit(name, err) {
            if (name == "") return
            if (err == "") err = "(no explicit error line; see log)"
            printf "%s\t%s\n", name, err
        }
        /^=== RUN   / { name = $3; err = ""; next }
        /^--- FAIL: / { emit(name, err); name = ""; err = ""; next }
        name != "" {
            if ($0 ~ /_test\.go:[0-9]+:/) {
                msg = $0
                sub(/^[[:space:]]*[^[:space:]:]+_test\.go:[0-9]+:[[:space:]]*/, "", msg)
                if (msg !~ /^(測試案例|Test case)/ && msg != "" && err == "") err = msg
            } else if ($0 ~ /^(panic:|fatal error:)/ && err == "") {
                err = $0
            }
        }
    ')"
    [[ -n "$block" ]] && failed_report+="$block"$'\n'
done
failed_report="$(printf '%s' "$failed_report" | sed '/^[[:space:]]*$/d')"

# 2) Root cause hints: first matching non-noise line per hint.
env_report=""
for entry in "${env_hints[@]}"; do
    pat="${entry%%|*}"; hint="${entry##*|}"
    sample=""
    while IFS= read -r ln; do
        [[ -z "$ln" ]] && continue
        if ! is_noise "$ln"; then sample="$ln"; break; fi
    done < <(grep -ahiE "$pat" "${logs[@]}" 2>/dev/null | strip_ansi | head -5 || true)
    [[ -n "$sample" ]] && env_report+="$hint :: $sample"$'\n'
done
env_report="$(printf '%s' "$env_report" | sed '/^[[:space:]]*$/d')"

# 3) Noise count, so readers know it was intentionally hidden, not missed.
noise_count=0
for log in "${logs[@]}"; do
    [[ -f "$log" ]] || continue
    n="$(grep -acE "$noise_re" "$log" 2>/dev/null || true)"
    noise_count=$((noise_count + ${n:-0}))
done

failed_names="$(printf '%s' "$failed_report" | cut -f1 | sort -u | paste -sd, - || true)"

# --- Annotations ---------------------------------------------------------------
echo "::error title=$(escape_property "$title failed")::$(escape_annotation "exit=$ret; failed tests: ${failed_names:-none}")"

while IFS=$'\t' read -r name err; do
    [[ -z "$name" ]] && continue
    echo "::error title=$(escape_property "$name")::$(escape_annotation "$err")"
done <<<"$failed_report"

while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    echo "::warning title=$(escape_property "可能的根因 (root cause)")::$(escape_annotation "$line")"
done <<<"$env_report"

# --- Job summary ---------------------------------------------------------------
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
    {
        echo "## $title failed"
        echo
        echo "Exit code: \`$ret\`"
        echo
        echo "### Failed tests"
        if [[ -n "$failed_report" ]]; then
            echo "| Test | Error |"
            echo "| --- | --- |"
            while IFS=$'\t' read -r name err; do
                [[ -z "$name" ]] && continue
                echo "| \`$name\` | $(printf '%s' "$err" | sed 's/|/\\|/g') |"
            done <<<"$failed_report"
        else
            echo "_No \`--- FAIL:\` block found in the test log._"
        fi
        echo
        if [[ -n "$env_report" ]]; then
            echo "### Root cause hints (environment / fixture)"
            echo '```text'
            echo "$env_report"
            echo '```'
            echo
        fi
        echo "Filtered benign/noise lines: \`$noise_count\`"
        echo
        echo "### Logs"
        for log in "${logs[@]}"; do
            [[ -f "$log" ]] && echo "- \`$log\`"
        done
    } >>"$GITHUB_STEP_SUMMARY"
fi

exit 0
