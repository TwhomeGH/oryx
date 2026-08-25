#!/bin/bash
# Temporary helper: verify the api_test.go major-version fix and camera scenario
# against local oryx7 (SRS7) container on oryx-test-net network.
set +e
cd /test

echo "=== API suite (no media) ==="
./oryx.test -test.timeout=10m -test.failfast -test.v -endpoint http://oryx7:2022 \
  -srs-log=true -wait-ready=true -init-password=false -init-self-signed-cert=false \
  -check-api-secret=true -no-media-test > /tmp/b2.log 2>&1
echo "B2=$?"
grep -E '--- FAIL' /tmp/b2.log | head -5
grep -E '--- PASS: TestApi_SrsApiWithAuth' /tmp/b2.log

echo "=== Camera scenario ==="
./oryx.test -test.timeout=10m -test.failfast -test.v -endpoint http://oryx7:2022 \
  -srs-log=true -wait-ready=true -init-password=false -init-self-signed-cert=false \
  -check-api-secret=true -test.run 'TestScenario_WithStream_PublishCameraStreamUrl' \
  -endpoint-rtmp rtmp://oryx7:1935 -endpoint-srt srt://oryx7:10080 -endpoint-http http://oryx7:2022 > /tmp/b4.log 2>&1
echo "B4=$?"
grep -E '--- FAIL|--- PASS' /tmp/b4.log | head -3
