# Temporary local test runner: golang + ffmpeg, mirrors CI's test.yml environment.
FROM ossrs/srs:tools AS tools
FROM golang:1.21
COPY --from=tools /usr/local/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --from=tools /usr/local/bin/ffprobe /usr/local/bin/ffprobe
