#!/bin/bash
echo "--- M1: 單行 select ---"
jq -c 'select(.id==1)' /tmp/v.jsonl
echo "--- M2: tags 展開 ---"
jq -c '.metadata.container.tags' /tmp/v.jsonl
echo "--- M3: test regex on tags ---"
jq -c '.metadata.container.tags | map(select(test("^sha-[0-9a-f]")))' /tmp/v.jsonl
echo "--- M4: any 形式 ---"
jq -c '{id: .id, m: (.metadata.container.tags // [] | any(test("^sha-[0-9a-f]")))}' /tmp/v.jsonl
echo "--- M5: 完整 collect 加 -s ---"
jq -s -c '[.[] | select((.metadata.container.tags // []) | any(test("^sha-[0-9a-f]"))) | .id]' /tmp/v.jsonl
