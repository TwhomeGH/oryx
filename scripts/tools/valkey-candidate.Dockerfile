# Redis 5 -> Valkey candidate; build/test locally before changing production.
ARG VALKEY_IMAGE=valkey/valkey:8.1.10@sha256:3fbd2e3e4b6e85e046c1e7c215e8f79087bc0357789184305806664e320996f3
ARG ORYX_IMAGE=ghcr.io/twhomegh/oryx:latest
FROM ${VALKEY_IMAGE} AS dependencies
COPY scripts/tools/prepare-valkey-runtime.sh /prepare-valkey-runtime.sh
RUN sh /prepare-valkey-runtime.sh

FROM ${ORYX_IMAGE} AS candidate
COPY --from=dependencies /runtime /opt/oryx/valkey
COPY scripts/tools/valkey-COPYING /opt/oryx/valkey/licenses/Valkey-COPYING
COPY scripts/tools/valkey-wrapper.sh /usr/local/bin/redis-server
COPY scripts/tools/valkey-wrapper.sh /usr/local/bin/redis-cli
COPY scripts/tools/valkey-wrapper.sh /usr/local/bin/valkey-server
COPY scripts/tools/valkey-wrapper.sh /usr/local/bin/valkey-cli
RUN chmod 755 /usr/local/bin/redis-server /usr/local/bin/redis-cli /usr/local/bin/valkey-server /usr/local/bin/valkey-cli && \
    redis-server --version && redis-cli --version && valkey-server --version && valkey-cli --version
