#!/bin/sh
set -eu
runtime=/opt/oryx/redis72
binary=${0##*/}
case "$binary" in
    redis-server|redis-cli) ;;
    *) echo "Unsupported Redis executable: $binary" >&2; exit 1 ;;
esac
export OPENSSL_CONF="${OPENSSL_CONF:-$runtime/openssl.cnf}"
export OPENSSL_MODULES="${OPENSSL_MODULES:-$runtime/ossl-modules}"
# Keep the kernel process name compatible with existing pidof redis-server checks.
exec "$runtime/loader/$binary" --argv0 "$binary" --library-path "$runtime/lib" "$runtime/bin/$binary" "$@"
