#!/bin/sh
set -eu
runtime=/opt/oryx/valkey
name=${0##*/}
case "$name" in
    redis-server|valkey-server) binary=valkey-server ;;
    redis-cli|valkey-cli) binary=valkey-cli ;;
    *) echo "Unsupported Valkey executable: $name" >&2; exit 1 ;;
esac
export OPENSSL_CONF="${OPENSSL_CONF:-$runtime/openssl.cnf}"
export OPENSSL_MODULES="${OPENSSL_MODULES:-$runtime/ossl-modules}"
# Legacy entry points retain their process names for the existing shutdown checks.
exec "$runtime/loader/$name" --argv0 "$name" --library-path "$runtime/lib" "$runtime/bin/$binary" "$@"
