#!/bin/bash
cd /ui
for p in source-map-url sane uglify-es uuid whatwg-encoding @humanwhocodes/config-array eslint; do
  echo "=== $p ==="
  npm ls "$p" 2>&1 | grep -v '^$' | head -8
done
