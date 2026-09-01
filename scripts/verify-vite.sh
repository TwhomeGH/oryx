#!/bin/bash
set -e
cd /ui
rm -rf build
make build > /tmp/build.log 2>&1
echo "MAKE_OK"
ls -d build
grep -o '/mgmt/assets/[^"]*' build/index.html | head -2
echo "=== VITEST ==="
npm run test 2>&1 | tail -6
