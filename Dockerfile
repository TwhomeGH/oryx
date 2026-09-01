ARG ARCH

FROM ${ARCH}node:22 AS node
FROM ${ARCH}ossrs/srs:7 AS srs

RUN mv /usr/local/srs/objs/ffmpeg/bin/ffmpeg /usr/local/bin/ffmpeg && \
    ln -sf /usr/local/bin/ffmpeg /usr/local/srs/objs/ffmpeg/bin/ffmpeg

RUN rm -rf /usr/local/srs/objs/nginx/html/console \
    /usr/local/srs/objs/nginx/html/players

FROM ${ARCH}ossrs/srs:ubuntu20 AS build

ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETARCH
RUN echo "BUILDPLATFORM: $BUILDPLATFORM, TARGETPLATFORM: $TARGETPLATFORM, TARGETARCH: $TARGETARCH"

# For ui build.
COPY --from=node /usr/local/bin /usr/local/bin
COPY --from=node /usr/local/lib /usr/local/lib
# For SRS server, always use the latest release version.
COPY --from=srs /usr/local/srs /usr/local/srs

ADD releases /g/releases
ADD mgmt /g/mgmt
ADD platform /g/platform
ADD ui /g/ui
ADD usr /g/usr
ADD test /g/test
ADD Makefile /g/Makefile

# For node to use more memory to fix: JavaScript heap out of memory
ENV NODE_OPTIONS="--max-old-space-size=4096"

# The UI is always built from the source in the build context (make build includes
# make -C ui), never from a pre-built ui/build: a cached or stale pre-built bundle
# would silently ship an outdated UI. BuildKit caches this RUN step by its inputs,
# so repeat builds with an unchanged ui/ are a cache hit and stay fast.
WORKDIR /g
RUN export SRS_NO_LINT=1 && \
    make clean && make -j build && make install

# UPX compression (--best --lzma) of the srs/platform binaries was removed:
# UPX-LZMA self-extraction crashes intermittently at process startup on
# GitHub Actions runner kernels (zero output, non-zero exit), which took down
# the CI integration test ("Start SRS failed"). See docs/local-test.md#FAQ.

# For youtube-dl, see https://github.com/ytdl-org/ytdl-nightly
FROM ${ARCH}python:3.9-slim-bullseye AS ytdl

RUN apt-get update -y && apt-get install -y binutils curl unzip && \
    pip install pyinstaller

WORKDIR /g
RUN curl -O -L https://github.com/ytdl-org/youtube-dl/archive/refs/heads/master.zip && \
    unzip -q master.zip && cd youtube-dl-master && \
    pyinstaller --onefile --clean --noconfirm --name youtube-dl youtube_dl/__main__.py && \
    cp dist/youtube-dl /usr/local/bin/ && \
    ldd /usr/local/bin/youtube-dl

# Full-featured ffmpeg+ffprobe from BtbN GPL builds, with hardware
# encoders (nvenc/qsv/vaapi/amf) compiled in. Arch selected by TARGETARCH.
# https://github.com/BtbN/FFmpeg-Builds/releases
FROM debian:bookworm-slim AS ffmpeg-full
ARG TARGETARCH
RUN apt-get update -y && apt-get install -y --no-install-recommends curl xz-utils ca-certificates && \
    case "${TARGETARCH}" in \
      amd64) FFB=ffmpeg-master-latest-linux64-gpl.tar.xz ;; \
      arm64) FFB=ffmpeg-master-latest-linuxarm64-gpl.tar.xz ;; \
      *) echo "unsupported arch ${TARGETARCH}" && exit 1 ;; \
    esac && \
    curl -fsSL "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/${FFB}" \
      | tar -xJ --strip-components=2 -C /usr/local/bin \
        "$(echo ${FFB} | sed 's/[.]tar[.]xz//')/bin/ffmpeg" \
        "$(echo ${FFB} | sed 's/[.]tar[.]xz//')/bin/ffprobe" && \
    chmod +x /usr/local/bin/ffmpeg /usr/local/bin/ffprobe

# http://releases.ubuntu.com/focal/
#FROM ${ARCH}ubuntu:focal AS dist
FROM ${ARCH}ossrs/oryx:focal-1 AS dist

# Expose ports @see https://github.com/ossrs/oryx/blob/main/DEVELOPER.md#docker-allocated-ports
EXPOSE 2022 2443 1935 8080 5060 9000 8000/udp 10080/udp

# Copy files from build.
COPY --from=build /usr/local/oryx /usr/local/oryx
COPY --from=build /usr/local/srs /usr/local/srs
COPY --from=ytdl /usr/local/bin/youtube-dl /usr/local/bin/

# Swap in full-featured ffmpeg/ffprobe; keep the previous fit-build as a
# fallback binary so it can be restored by swapping filenames if ever needed.
RUN mv /usr/local/bin/ffmpeg /usr/local/bin/ffmpeg-fit && \
    mv /usr/local/bin/ffprobe /usr/local/bin/ffprobe-fit || true
    COPY --from=ffmpeg-full /usr/local/bin/ffmpeg /usr/local/bin/ffmpeg
    COPY --from=ffmpeg-full /usr/local/bin/ffprobe /usr/local/bin/ffprobe
# Runtime libraries for hardware encoding:
# - VAAPI/QSV: libva/libdrm client libs, libva-drm DRM backend, mesa/intel userspace drivers.
# - NVENC: NOT baked in. libnvidia-encode must match the host driver's nvenc API
#   version, so we rely on the nvidia-container-toolkit runtime to mount the host
#   driver's libraries (native Linux), or the user mounts them manually (WSL2,
#   where the runtime doesn't auto-mount them). A baked version (e.g. 570) goes
#   stale as the host driver updates and breaks the nvenc API handshake.
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y --no-install-recommends \
    libva2 libva-drm2 libdrm2 libx11-6 \
    mesa-va-drivers intel-media-va-driver && rm -rf /var/lib/apt/lists/*

# Prepare data directory.
RUN mkdir -p /data && \
    cd /usr/local/oryx/platform/containers && \
    rm -rf data && ln -sf /data .

# Ensure platform/objs exists for SRS pid file. The repo ships it as a Git
# symlink to containers/objs; on Windows the checkout degrades and BuildKit
# turns junctions into dangling absolute-path symlinks, so rebuild it here.
RUN cd /usr/local/oryx/platform && \
    mkdir -p containers/objs && \
    rm -rf objs && \
    ln -s containers/objs objs

CMD ["./bootstrap"]
