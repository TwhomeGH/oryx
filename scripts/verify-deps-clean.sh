#!/bin/bash
set -e
cd /ui
rm -rf node_modules package-lock.json
npm install --no-audit --no-fund 2>&1 | grep -E 'deprecated|added' | head -12
echo "=== LINT ==="
npm run lint 2>&1 | tail -15 || true
echo "=== BUILD ==="
npx vite build 2>&1 | grep -E 'built in|error during'
echo "=== TEST ==="
npx vitest run 2>&1 | grep -E 'Test Files|Tests '
echo "=== COUNT ==="
ls node_modules | wc -l
