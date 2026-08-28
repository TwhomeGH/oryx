#!/bin/sh
set -e
# Temporary helper: validate all locale_*.json files and parse edited JSX files with esbuild.
node -e "const fs=require('fs'); const d='/src/resources'; fs.readdirSync(d).filter(f=>/^locale_.*\.json$/.test(f)).forEach(f=>{ JSON.parse(fs.readFileSync(d+'/'+f,'utf8')); console.log('LOCALE_OK '+f); });"
npx --yes esbuild /src/pages/Components.js /src/pages/ScenarioForward.js /src/pages/Settings.js --loader:.js=jsx --outdir=/tmp/out --log-level=error
echo "JSX_OK"
