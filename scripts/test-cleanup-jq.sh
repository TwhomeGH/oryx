#!/bin/bash
cat > /tmp/v.jsonl <<'EOF'
{"id":1,"created_at":"2026-01-01T00:00:00Z","metadata":{"container":{"tags":["sha-abc1234"]}}}
{"id":2,"created_at":"2026-02-01T00:00:00Z","metadata":{"container":{"tags":["latest","v5.15.20"]}}}
{"id":3,"created_at":"2026-03-01T00:00:00Z","metadata":{"container":{"tags":[]}}}
EOF
echo "=== untagged (expect 3) ==="
jq -r 'select((.metadata.container.tags // []) | length == 0) | .id' /tmp/v.jsonl
echo "=== sha beyond keep=1 (expect empty) ==="
jq -r --argjson keep 1 '[.[] | select((.metadata.container.tags // []) | any(test("^sha-[0-9a-f]")))] | sort_by(.created_at) | reverse | .[$keep:][] | .id' /tmp/v.jsonl
echo "=== sha beyond keep=0 (expect 1) ==="
jq -r --argjson keep 0 '[.[] | select((.metadata.container.tags // []) | any(test("^sha-[0-9a-f]")))] | sort_by(.created_at) | reverse | .[$keep:][] | .id' /tmp/v.jsonl
echo "=== release tags (expect 2, never deleted) ==="
jq -r '.[] | select((.metadata.container.tags // []) | any(test("^(latest|v[0-9])"))) | .id' /tmp/v.jsonl
