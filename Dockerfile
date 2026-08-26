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
ARG MAKEARGS
RUN echo "BUILDPLATFORM: $BUILDPLATFORM, TARGETPLATFORM: $TARGETPLATFORM, TARGETARCH: $TARGETARCH, MAKEARGS: $MAKEARGS"

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

# By default, make all, including platform and ui, but it will take a long time,
# so there is a MAKEARGS to build without UI, see platform.yml.
WORKDIR /g
# We define SRS_NO_LINT to disable the lint check.
RUN export SRS_NO_LINT=1 && \
    make clean && make -j ${MAKEARGS} && make install

# Use UPX to compress the binary.
# https://serverfault.com/questions/949991/how-to-install-tzdata-on-a-ubuntu-docker-image
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y upx

RUN echo "Before UPX for $TARGETARCH" && \
    ls -lh /usr/local/srs/objs/srs /usr/local/oryx/platform/platform && \
    upx --best --lzma /usr/local/srs/objs/srs && \
    upx --best --lzma /usr/local/oryx/platform/platform && \
    echo "After UPX for $TARGETARCH" && \
    ls -lh /usr/local/srs/objs/srs /usr/local/oryx/platform/platform

# For youtube-dl, see https://github.com/ytdl-org/ytdl-nightly
FROM ${ARCH}python:3.9-slim-bullseye AS ytdl

# Full-featured static ffmpeg+ffprobe, adds hardware encoders (nvenc/vaapi)
# and many more codecs/filters over the srs fit-build.
# https://github.com/mwader/static-ffmpeg
FROM ${ARCH}mwader/static-ffmpeg:6.1 AS ffmpeg-full

RUN apt-get update -y && apt-get install -y binutils curl unzip && \
    pip install pyinstaller

WORKDIR /g
RUN curl -O -L https://github.com/ytdl-org/youtube-dl/archive/refs/heads/master.zip && \
    unzip -q master.zip && cd youtube-dl-master && \
    pyinstaller --onefile --clean --noconfirm --name youtube-dl youtube_dl/__main__.py && \
    cp dist/youtube-dl /usr/local/bin/ && \
    ldd /usr/local/bin/youtube-dl

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
COPY --from=ffmpeg-full /ffmpeg /usr/local/bin/ffmpeg
COPY --from=ffmpeg-full /ffprobe /usr/local/bin/ffprobe
# Runtime libraries for VAAPI (AMD/Intel iGPU) hardware encoding.
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y --no-install-recommends \
    libva2 libdrm2 && rm -rf /var/lib/apt/lists/*

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
