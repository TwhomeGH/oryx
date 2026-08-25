#!/bin/sh
set -e
# Temporary helper: validate locale.json and parse edited JSX files with esbuild.
node -e "JSON.parse(require('fs').readFileSync('/src/resources/locale.json','utf8')); console.log('LOCALE_OK')"
npx --yes esbuild /src/pages/Components.js /src/pages/ScenarioForward.js --loader:.js=jsx --outdir=/tmp/out --log-level=error
echo "JSX_OK"
