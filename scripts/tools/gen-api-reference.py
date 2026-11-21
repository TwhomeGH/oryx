#!/usr/bin/env python3
"""Generate docs/api-reference.md from the platform's HTTP handler registrations.

Why this exists: the platform exposes ~150 /terraform/v1 endpoints and the docs mentioned only a
fraction of them, so "what API do I call" had no answer short of reading the source. This script
derives the index from the code itself, so it cannot drift silently; --check fails CI when the file
is stale.

Usage:
  python scripts/tools/gen-api-reference.py            # rewrite docs/api-reference.md
  python scripts/tools/gen-api-reference.py --check    # exit 1 if the file is out of date
"""

import io
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PLATFORM = os.path.join(ROOT, "platform")
OUT = os.path.join(ROOT, "docs", "api-reference.md")

FUNC_RE = re.compile(r"^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(", re.M)
PATH_RE = re.compile(r'"(/terraform/v1/[A-Za-z0-9/_\-{}.:]+)"')
CASE_RE = re.compile(r'^\s*case\s+"([^"]+)"\s*:', re.M)


def doc_comment(lines, func_line_index):
    """Collect the contiguous // block immediately above the function."""
    out = []
    i = func_line_index - 1
    while i >= 0 and lines[i].strip().startswith("//"):
        out.append(lines[i].strip()[2:].strip())
        i -= 1
    out.reverse()
    return out


def summarize(comment, func):
    """First sentence of the doc comment, without the leading function name."""
    if not comment:
        return ""
    text = " ".join(comment)
    text = re.sub(r"^%s\s+" % re.escape(func), "", text)
    # Cut at the first sentence end, then trim a trailing dash/label.
    m = re.match(r"(.+?[.!?。])(\s|$)", text)
    if m:
        text = m.group(1)
    text = text.strip(" .。")
    if len(text) > 160:
        text = text[:157] + "…"
    return text.replace("|", "\\|")


def group_of(path):
    parts = [p for p in path.split("/") if p]
    rest = parts[2:]
    return rest[0] if rest else "other"


def auth_for(body, path):
    """Auth is per registration, not per function: one function can expose both authenticated and
    public routes, so looking for a middleware anywhere in the function mislabels them."""
    results = []
    for m in re.finditer(re.escape('"%s"' % path), body):
        window = body[m.start():m.start() + 400]
        if "middlewareAuthTokenInBody(" in window:
            results.append("body")
        elif "middlewareAuthTokenInURL(" in window:
            results.append("url")
        elif "handler.HandleFunc(" in window or "handler.Handle(" in window:
            results.append("open")
    if not results:
        return "unknown"
    uniq = sorted(set(results))
    return uniq[0] if len(uniq) == 1 else "mixed"


def ep_comment(lines, func_start, body, path):
    """A comment directly above `ep := "<path>"` is often the only description a worker provides."""
    needle = '"%s"' % path
    pos = body.find(needle)
    if pos < 0:
        return ""
    line_no = func_start + body[:pos].count("\n")
    out = []
    i = line_no - 1
    while i >= 0 and lines[i].strip().startswith("//"):
        out.append(lines[i].strip()[2:].strip())
        i -= 1
    out.reverse()
    text = " ".join(out).strip()
    if len(text) > 160:
        text = text[:157] + "…"
    return text.replace("|", "\\|")


def doc_links(paths):
    """Map endpoint -> docs that mention it. Cheap, and it shows where prose already exists."""
    out = {p: [] for p in paths}
    docs_dir = os.path.join(ROOT, "docs")
    for name in sorted(os.listdir(docs_dir)):
        if not name.endswith(".md") or name == "api-reference.md":
            # The generated file itself mentions every path; including it would make every row claim
            # to be documented.
            continue
        try:
            text = io.open(os.path.join(docs_dir, name), encoding="utf-8").read()
        except OSError:
            continue
        for p in paths:
            if p in text:
                out[p].append(name)
    return out


def scan():
    endpoints = []
    for name in sorted(os.listdir(PLATFORM)):
        if not name.endswith(".go") or name.endswith("_test.go"):
            continue
        fp = os.path.join(PLATFORM, name)
        text = io.open(fp, encoding="utf-8").read()
        lines = text.split("\n")

        matches = list(FUNC_RE.finditer(text))
        for idx, m in enumerate(matches):
            func = m.group(1)
            start = text[: m.start()].count("\n")
            end = matches[idx + 1].start() if idx + 1 < len(matches) else len(text)
            body = text[m.start():end]
            paths = sorted(set(PATH_RE.findall(body)))
            if not paths:
                continue

            actions = sorted(set(CASE_RE.findall(body)))
            doc_desc = summarize(doc_comment(lines, start), func)
            impl = func
            recv = re.match(r"func\s+\([^)]*\*([A-Za-z0-9_]+)\)", body)
            if recv:
                impl = recv.group(1) + "." + func
            for p in paths:
                desc = doc_desc or ep_comment(lines, start, body, p)
                endpoints.append({
                    "path": p,
                    "group": group_of(p),
                    "auth": auth_for(body, p),
                    "actions": actions,
                    "desc": desc,
                    "func": impl,
                    "file": name,
                })

    # One row per endpoint; keep the first registration (some are registered twice for aliases).
    seen = {}
    for e in endpoints:
        seen.setdefault(e["path"], e)
    return list(seen.values())


AUTH_LABEL = {
    "body": "body token",
    "url": "query token",
    "open": "—",
    "mixed": "body token / 公開混用",
    "unknown": "?",
}


def render(items):
    items.sort(key=lambda e: (e["group"], e["path"]))
    by_group = {}
    for e in items:
        by_group.setdefault(e["group"], []).append(e)

    out = []
    out.append("# API 參考（自動產生）")
    out.append("")
    out.append("> 本檔由 `scripts/tools/gen-api-reference.py` 掃描 `platform/*.go` 的註冊點產生，"
               "**請勿手改**。")
    out.append(">")
    out.append("> 更新：`python scripts/tools/gen-api-reference.py`；CI 以 `--check` 驗證是否過期。")
    out.append("")
    out.append("共 **%d** 個 `/terraform/v1` 端點。認證欄位：" % len(items))
    out.append("")
    out.append("| 值 | 意義 |")
    out.append("|---|---|")
    out.append("| `body token` | 請求 body 需帶 `token`（`middlewareAuthTokenInBody`） |")
    out.append("| `query token` | 以查詢參數帶 token（`middlewareAuthTokenInURL`） |")
    out.append("| `—` | 沒有掛中介層；可能是 SRS hook（由 SRS 直接呼叫）或刻意公開 |")
    out.append("")
    out.append("動作欄位列出該處理函式 `switch action` 的分支值；沒有分支表示單一用途端點。")
    out.append("")
    missing = sum(1 for e in items if not e["desc"])
    out.append("說明欄為 `—` 的端點有 **%d** 個（該處理函式沒有註解）—— 這是目前的可見文件債。" % missing)
    out.append("")

    for group in sorted(by_group):
        rows = by_group[group]
        out.append("## %s（%d）" % (group, len(rows)))
        out.append("")
        out.append("| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |")
        out.append("|---|---|---|---|---|---|")
        for e in rows:
            links = ", ".join("`%s`" % d for d in e["docs"]) if e.get("docs") else "—"
            out.append("| `%s` | %s | %s | %s | %s | `%s`（%s） |" % (
                e["path"],
                ", ".join(e["actions"]) if e["actions"] else "—",
                AUTH_LABEL.get(e["auth"], e["auth"]),
                e["desc"] or "—",
                links,
                e["func"],
                e["file"],
            ))
        out.append("")

    out.append("---")
    out.append("")
    out.append("## 還缺什麼")
    out.append("")
    out.append("- 這份索引只回答「有哪些端點、怎麼認證、有哪些動作」。")
    out.append("- 完整 schema（請求／回應欄位）尚未產生；核心端點見各 `docs/*.md` 與程式內 `ParseBody` 結構。")
    out.append("- 端點若在程式裡改名或新增而沒有重新產生本檔，`--check` 會讓 CI 失敗。")
    out.append("")
    return "\n".join(out)


def main():
    items = scan()
    links = doc_links([e["path"] for e in items])
    for e in items:
        e["docs"] = links.get(e["path"], [])
    body = render(items)
    check = "--check" in sys.argv

    if check:
        current = ""
        if os.path.exists(OUT):
            current = io.open(OUT, encoding="utf-8").read()
        if current != body:
            print("docs/api-reference.md is out of date.")
            print("Run: python scripts/tools/gen-api-reference.py")
            return 1
        print("docs/api-reference.md is up to date (%d endpoints)." % len(items))
        return 0

    io.open(OUT, "w", encoding="utf-8", newline="\n").write(body)
    print("wrote %s (%d endpoints)" % (OUT, len(items)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
