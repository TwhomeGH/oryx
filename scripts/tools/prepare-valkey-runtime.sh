#!/bin/sh
# Run inside the pinned official Valkey image; shared by production and rehearsal.
set -eu
mkdir -p /runtime/bin /runtime/lib /runtime/loader /runtime/licenses /runtime/ossl-modules
cp /usr/local/bin/valkey-server /usr/local/bin/valkey-cli /runtime/bin/
for binary in /runtime/bin/*; do
    ldd "$binary" | awk '/=> \// {print $3}' |
        while read -r library; do cp -L "$library" /runtime/lib/; done
done
loader="$(ldd /runtime/bin/valkey-server | awk '/ld-linux/ {print $1}')"
for name in redis-server redis-cli valkey-server valkey-cli; do
    cp -L "$loader" "/runtime/loader/$name"
done
ssl_library="$(ldd /runtime/bin/valkey-server | awk '/libssl.so.3 =>/ {print $3}')"
cp -a "$(dirname "$ssl_library")/ossl-modules/." /runtime/ossl-modules/
if [ -f /etc/ssl/openssl.cnf ]; then
    cp /etc/ssl/openssl.cnf /runtime/openssl.cnf
else
    touch /runtime/openssl.cnf
fi
for package in libc6 libssl3t64 libsystemd0 libcap2 zlib1g libzstd1; do
    cp "/usr/share/doc/$package/copyright" "/runtime/licenses/$package-copyright"
    dpkg-query -W "$package" >> /runtime/licenses/package-versions.txt
done
