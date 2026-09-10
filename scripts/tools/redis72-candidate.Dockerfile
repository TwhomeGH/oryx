# Local candidate only; does not change the production image build.
ARG REDIS_IMAGE=redis:7.2.16@sha256:74566c6910d13ae61e7ce73ebd3127438a1fe805b309b097c323142719ec8a5b
ARG ORYX_IMAGE=ghcr.io/twhomegh/oryx:latest
FROM ${REDIS_IMAGE} AS dependencies
RUN set -eu; \
    mkdir -p /runtime/bin /runtime/lib /runtime/loader /runtime/licenses /runtime/ossl-modules; \
    cp /usr/local/bin/redis-server /usr/local/bin/redis-cli /runtime/bin/; \
    for binary in /runtime/bin/*; do \
      ldd "$binary" | awk '/=> \// {print $3}' | \
        while read -r library; do cp -L "$library" /runtime/lib/; done; \
    done; \
    loader="$(ldd /runtime/bin/redis-server | awk '/ld-linux/ {print $1}')"; \
    cp -L "$loader" /runtime/lib/ld.so; \
    cp -L "$loader" /runtime/loader/redis-server; \
    cp -L "$loader" /runtime/loader/redis-cli; \
    ssl_library="$(ldd /runtime/bin/redis-server | awk '/libssl.so.3 =>/ {print $3}')"; \
    cp -a "$(dirname "$ssl_library")/ossl-modules/." /runtime/ossl-modules/; \
    if [ -f /etc/ssl/openssl.cnf ]; then cp /etc/ssl/openssl.cnf /runtime/openssl.cnf; \
      else touch /runtime/openssl.cnf; fi; \
    cp /usr/share/doc/libssl3/copyright /runtime/licenses/OpenSSL-copyright; \
    cp /usr/share/doc/libc6/copyright /runtime/licenses/glibc-copyright

# Diagnostic target: demonstrate what remains after supplying OpenSSL 3 alone.
FROM ${ORYX_IMAGE} AS openssl-only
COPY --from=dependencies /runtime/lib/libssl.so.3 /runtime/lib/libcrypto.so.3 /opt/redis-ssl/
COPY --from=dependencies /runtime/bin/redis-server /opt/redis-ssl/redis-server
ENV LD_LIBRARY_PATH=/opt/redis-ssl
ENTRYPOINT ["/opt/redis-ssl/redis-server"]
CMD ["--version"]

# Keep the new loader/libc private to Redis; never overwrite Oryx's system libraries.
FROM ${ORYX_IMAGE} AS candidate
COPY --from=dependencies /runtime /opt/oryx/redis72
COPY scripts/tools/redis72-COPYING /opt/oryx/redis72/licenses/Redis-COPYING
COPY scripts/tools/redis72-wrapper.sh /usr/local/bin/redis-server
COPY scripts/tools/redis72-wrapper.sh /usr/local/bin/redis-cli
RUN chmod 755 /usr/local/bin/redis-server /usr/local/bin/redis-cli && \
    redis-server --version && redis-cli --version
