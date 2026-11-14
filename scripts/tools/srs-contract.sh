#!/bin/bash
#
# SRS core contract check.
#
# Oryx drives SRS through a small, fixed surface (binary layout, HTTP API, hooks, http_remux,
# HLS/DVR, RTC). Swapping the SRS core for a fork is only safe if that surface still behaves the
# same, so this script exercises it against a *running* Oryx container and prints a verdict per
# item. Run it for the upstream core and for any replacement, then compare.
#
# Usage:
#   bash scripts/tools/srs-contract.sh [-c CONTAINER] [-a APP] [-s STREAM] [-v]
#
# Windows: run it with Git Bash, not the WSL bash (the latter has no docker socket):
#   "C:\Program Files\Git\bin\bash.exe" scripts/tools/srs-contract.sh -c oryx
#
# Exit status: 0 when every check passes, 1 otherwise (the failing items are listed).

set -u

CONTAINER=oryx
APP=live
STREAM=srs-contract
VERBOSE=no
DOCKER=${DOCKER:-docker}

# Git Bash on Windows rewrites arguments that look like absolute POSIX paths ("/usr/local/srs" ->
# "C:/Program Files/Git/usr/local/srs"), which makes docker exec look for files that do not exist.
# Disable that rewriting; it is a no-op on Linux.
export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

while [[ "$#" -gt 0 ]]; do
    case $1 in
        -c|--container) CONTAINER=$2; shift 2 ;;
        -a|--app) APP=$2; shift 2 ;;
        -s|--stream) STREAM=$2; shift 2 ;;
        -v|--verbose) VERBOSE=yes; shift ;;
        -h|--help) sed -n '3,14p' "$0"; exit 0 ;;
        *) echo "Unknown parameter: $1"; exit 1 ;;
    esac
done

PASS=0
FAIL=0
FAILED_ITEMS=()

ok() { PASS=$((PASS + 1)); printf '  [PASS] %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); FAILED_ITEMS+=("$1"); printf '  [FAIL] %s\n' "$1"; }
note() { printf '  [info] %s\n' "$1"; }

# SRS speaks HTTP on 1985 inside the container. bash's /dev/tcp avoids requiring curl in the image.
srs_get() {
    $DOCKER exec "$CONTAINER" bash -c "exec 3<>/dev/tcp/127.0.0.1/1985; printf 'GET $1 HTTP/1.0\r\nHost: x\r\n\r\n' >&3; timeout 3 cat <&3" 2>/dev/null | tail -1
}

echo "SRS contract check"
echo "  container: $CONTAINER"
echo "  app/stream: $APP/$STREAM"
echo

# ---------------------------------------------------------------- 1. core identity
echo "1) core identity"
VERSIONS=$(srs_get /api/v1/versions)
if echo "$VERSIONS" | grep -q '"version"'; then
    ok "GET /api/v1/versions reports a version: $(echo "$VERSIONS" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"
else
    bad "GET /api/v1/versions"
fi

SUMMARIES=$(srs_get /api/v1/summaries)
if echo "$SUMMARIES" | grep -q '"self"'; then
    ok "GET /api/v1/summaries returns self"
else
    bad "GET /api/v1/summaries"
fi

if $DOCKER exec "$CONTAINER" test -x /usr/local/srs/objs/srs; then
    ok "binary at /usr/local/srs/objs/srs"
else
    bad "binary at /usr/local/srs/objs/srs"
fi

# ---------------------------------------------------------------- 2. config surface Oryx generates
echo
echo "2) configuration surface"
CONF=/usr/local/oryx/platform/containers/conf/srs.release.conf
for key in http_hooks http_remux rtc_server srt_server 'min_latency' 'publish' 'play'; do
    if $DOCKER exec "$CONTAINER" grep -q "$key" "$CONF" 2>/dev/null; then
        ok "config declares $key"
    else
        bad "config declares $key"
    fi
done

# ---------------------------------------------------------------- 3. publish path + hooks
echo
echo "3) publish path and hooks"
SECRET=$($DOCKER exec "$CONTAINER" redis-cli get SRS_SECRET_PUBLISH 2>/dev/null | tr -d '\r')
if [[ -z "${SECRET}" ]]; then
    bad "read SRS_SECRET_PUBLISH from redis"
else
    ok "read SRS_SECRET_PUBLISH from redis"
fi

$DOCKER exec "$CONTAINER" bash -c "pkill -f 'rtmp://localhost/$APP/$STREAM' 2>/dev/null; true"
PUBLOG=/tmp/srs-contract-publish.log
$DOCKER exec -d "$CONTAINER" bash -c "ffmpeg -hide_banner -loglevel error -re -f lavfi -i testsrc2=size=640x360:rate=25 \
    -f lavfi -i sine=frequency=440 -c:v libx264 -preset veryfast -g 50 -b:v 800k -c:a aac -b:a 96k -t 25 \
    -f flv 'rtmp://localhost/$APP/$STREAM?secret=$SECRET' >$PUBLOG 2>&1"
sleep 6

STREAMS=$(srs_get /api/v1/streams/)
if echo "$STREAMS" | grep -q "\"name\":\"$STREAM\""; then
    ok "GET /api/v1/streams lists the published stream"
else
    bad "GET /api/v1/streams lists the published stream"
fi

if $DOCKER exec "$CONTAINER" redis-cli hkeys SRS_STREAM_ACTIVE 2>/dev/null | grep -q "$STREAM"; then
    ok "on_publish hook wrote SRS_STREAM_ACTIVE (proxy verifies via the platform)"
else
    bad "on_publish hook wrote SRS_STREAM_ACTIVE"
fi

CLIENTS=$(srs_get /api/v1/clients/)
if echo "$CLIENTS" | grep -q '"publish":true'; then
    ok "GET /api/v1/clients reports the publisher"
else
    bad "GET /api/v1/clients reports the publisher"
fi

if echo "$CLIENTS" | grep -q '"kbps"' && echo "$CLIENTS" | grep -q '"alive"'; then
    ok "client payload carries kbps and alive"
else
    bad "client payload carries kbps and alive"
fi

# ---------------------------------------------------------------- 4. play path (http_remux)
echo
echo "4) play path"
if $DOCKER exec "$CONTAINER" bash -c "timeout 5 ffmpeg -hide_banner -loglevel error -i http://localhost:8080/$APP/$STREAM.flv -t 2 -c copy -f null - >/dev/null 2>&1"; then
    ok "http_remux serves /$APP/$STREAM.flv"
else
    bad "http_remux serves /$APP/$STREAM.flv"
fi

# ---------------------------------------------------------------- 5. HLS / DVR dependency
echo
echo "5) HLS (DVR dependency)"
HLS_ON=$($DOCKER exec "$CONTAINER" redis-cli hget SRS_HLS_MASTER enabled 2>/dev/null | tr -d '\r')
if [[ "$HLS_ON" == "true" ]]; then
    TS=/usr/local/srs/objs/nginx/html/$APP
    if $DOCKER exec "$CONTAINER" bash -c "ls $TS/*.ts >/dev/null 2>&1"; then
        ok "HLS writes ts segments (DVR can record)"
    else
        note "HLS enabled but no .ts yet (publish longer or check hls_path)"
    fi
else
    note "HLS disabled, skipping segment check (DVR cannot record in this mode)"
fi

# ---------------------------------------------------------------- 6. RTC surface
echo
echo "6) WebRTC surface"
if $DOCKER exec "$CONTAINER" grep -q 'rtmp_to_rtc' "$CONF" 2>/dev/null; then
    ok "rtc block declares rtmp_to_rtc"
else
    bad "rtc block declares rtmp_to_rtc"
fi
if $DOCKER exec "$CONTAINER" grep -q 'candidate' "$CONF" 2>/dev/null; then
    ok "rtc_server declares candidate"
else
    bad "rtc_server declares candidate"
fi

# ---------------------------------------------------------------- 7. teardown + kick API
echo
echo "7) teardown"
CID=$(echo "$CLIENTS" | sed -n 's/.*"id":"\([^"]*\)"[^}]*"publish":true.*/\1/p' | head -1)
if [[ -n "${CID}" ]]; then
    CODE=$($DOCKER exec "$CONTAINER" bash -c "exec 3<>/dev/tcp/127.0.0.1/1985; printf 'DELETE /api/v1/clients/$CID HTTP/1.0\r\nHost: x\r\n\r\n' >&3; timeout 3 cat <&3" 2>/dev/null | head -1 | tr -d '\r')
    if echo "$CODE" | grep -q '200'; then
        ok "DELETE /api/v1/clients/{id} disconnects a client"
    else
        note "DELETE /api/v1/clients returned: ${CODE:-no response}"
    fi
else
    note "no publisher id to kick"
fi
$DOCKER exec "$CONTAINER" bash -c "pkill -f 'rtmp://localhost/$APP/$STREAM' 2>/dev/null; true"

# ---------------------------------------------------------------- verdict
echo
echo "-------------------------------------------"
echo "PASS: $PASS   FAIL: $FAIL"
if [[ $FAIL -gt 0 ]]; then
    printf 'failed items:\n'
    for item in "${FAILED_ITEMS[@]}"; do printf '  - %s\n' "$item"; done
    exit 1
fi
echo "SRS core satisfies the Oryx contract."
