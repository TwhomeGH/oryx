#!/usr/bin/env python3
"""Generate ui/src/resources/locale_zh-TW.json from locale_zh.json via OpenCC (s2twp).

Only the Simplified locale (locale_zh.json) is maintained by hand; the Traditional one is
generated, so the two never drift. OpenCC's s2twp does Simplified -> Traditional (Taiwan) with
phrase conversion; a small OVERRIDES map fixes domain terms it gets wrong in this project.

Run: python scripts/tools/gen-zh-tw-locale.py
Requires: python -m pip install opencc
"""
import json
import os
import sys

try:
    import opencc
except ImportError:
    sys.exit("需要 OpenCC：python -m pip install opencc")

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
SRC = os.path.join(ROOT, "ui", "src", "resources", "locale_zh.json")
DST = os.path.join(ROOT, "ui", "src", "resources", "locale_zh-TW.json")

# s2twp output -> Taiwan term, for domain words it does not handle.
OVERRIDES = {
    "質量": "品質",
    "客戶端": "用戶端",
    "錄製": "錄影",
    "域名": "網域",
    "攝像頭": "攝影機",
    "攝像機": "攝影機",
    "回撥": "回呼",
    "端口": "連接埠",
    "依賴": "相依",
    "位元速率": "位元率",
}


def convert_tree(cc, node):
    """Recursively convert every string value (keys stay as-is)."""
    if isinstance(node, dict):
        return {k: convert_tree(cc, v) for k, v in node.items()}
    if isinstance(node, list):
        return [convert_tree(cc, v) for v in node]
    if isinstance(node, str):
        out = cc.convert(node)
        for a, b in OVERRIDES.items():
            out = out.replace(a, b)
        return out
    return node


def main():
    cc = opencc.OpenCC("s2twp")
    with open(SRC, encoding="utf-8") as f:
        data = json.load(f)

    out = {
        "translation": convert_tree(cc, data.get("translation", {})),
        "meta": {"code": "zh-TW", "name": "繁體中文"},
    }

    with open(DST, "w", encoding="utf-8", newline="\n") as f:
        json.dump(out, f, ensure_ascii=False, indent=2)
        f.write("\n")

    print(f"已產生 {os.path.relpath(DST, ROOT)}（{len(out['translation'])} 個頂層鍵）")


if __name__ == "__main__":
    main()
