#!/bin/bash
# Temporary helper: replicate test.yml "Check and Test service" blocks against a
# local oryx7 container (SRS7 image) over the oryx-test-net bridge network.
set +e
EP=http://oryx7:2022
EPS=https://oryx7:2443
cd /test

echo "=== BLOCK1: init + TestSystem_Empty ==="
./oryx.test -test.timeout=1h -test.failfast -test.v -endpoint $EP \
  -srs-log=true -wait-ready=true -init-password=true -init-self-signed-cert=true \
  -check-api-secret=true -test.run TestSystem_Empty 2>&1 | tail -25
r1=$?

echo "=== BLOCK2: HTTP service, no media ==="
./oryx.test -test.timeout=1h -test.failfast -test.v -endpoint $EP \
  -srs-log=true -wait-ready=true -init-password=false -init-self-signed-cert=false \
  -check-api-secret=true -no-media-test 2>&1 | tail -25
r2=$?

echo "=== BLOCK3: HTTPS service ==="
./oryx.test -test.timeout=1h -test.failfast -test.v -endpoint $EPS \
  -srs-log=true -wait-ready=true -init-password=false -init-self-signed-cert=false \
  -check-api-secret=true -no-media-test 2>&1 | tail -25
r3=$?

echo "=== BLOCK4: media WithStream ==="
./oryx.test -test.timeout=1h -test.failfast -test.v -endpoint $EP \
  -srs-log=true -wait-ready=true -init-password=false -init-self-signed-cert=false \
  -check-api-secret=true -test.run WithStream \
  -endpoint-rtmp rtmp://oryx7:1935 -endpoint-srt srt://oryx7:10080 2>&1 | tail -40
r4=$?

echo "RESULTS: b1=$r1 b2=$r2 b3=$r3 b4=$r4"
