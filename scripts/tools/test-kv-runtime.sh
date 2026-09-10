#!/bin/bash
# Run only in a disposable candidate container, without production mounts.
set -euo pipefail
cd /usr/local/oryx/platform
export REDIS_PASSWORD=isolated-upgrade-test
export REDISCLI_AUTH=isolated-upgrade-test
trap 'bash ./auto/stop_redis >/dev/null 2>&1 || true' EXIT
bash ./auto/start_redis >/tmp/redis-start.log 2>&1
test "$(redis-cli -h localhost PING)" = PONG
redis-cli INFO server | tr -d '\r' | grep -qx 'server_name:valkey'
valkey-cli INFO server | tr -d '\r' | grep -Eq '^valkey_version:[0-9]+\.[0-9]+\.[0-9]+$'
valkey-server --version
pidof redis-server >/dev/null
redis-cli SET candidate-runtime-check preserved >/dev/null
redis-cli SAVE >/dev/null
bash ./auto/stop_redis
test ! -f /var/run/redis/redis-server.pid
if pidof redis-server >/dev/null; then echo 'Redis did not stop' >&2; exit 1; fi
bash ./auto/start_redis >/tmp/redis-start.log 2>&1
test "$(redis-cli GET candidate-runtime-check)" = preserved
test "$(getconf GNU_LIBC_VERSION)" = 'glibc 2.31'
ffmpeg -version >/dev/null
ffprobe -version >/dev/null
redis-server --version
redis-cli --version
echo 'PASS: Valkey identity, original start/stop, pidof, localhost, SAVE/restart, system glibc, FFmpeg/ffprobe startup'
