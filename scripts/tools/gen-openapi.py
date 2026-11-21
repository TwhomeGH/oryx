#!/usr/bin/env python3
"""Build docs/openapi.json for the operational API.

The platform has ~150 endpoints; this spec covers the ones an operator or script actually calls
(forward, streams, record, disk, GPU, SRS health, versions). Schemas are inferred from real captured
responses, so field names and nesting come from the running service rather than from memory.

Why JSON and not YAML: the file is served as-is to a browser viewer and needs no parser anywhere
(no PyYAML in CI, no YAML bundle in the page). Regenerating needs live captures:

  python scripts/tools/gen-openapi.py --captures .tmp/spec

Without --captures the committed spec is left untouched; --check only validates that every documented
path still exists in the code (via docs/api-reference.md).
"""

import io
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "platform", "containers", "www", "api", "openapi.json")
API_REF = os.path.join(ROOT, "docs", "api-reference.md")

TAGS = [
    {"name": "forward", "description": "多平台轉播：目標設定、狀態、指標與執行紀錄。"},
    {"name": "streams", "description": "SRS 串流狀態與生命週期事件。"},
    {"name": "record", "description": "錄影（DVR）狀態。"},
    {"name": "system", "description": "平台自身的磁碟、GPU、SRS 日誌健康與版本資訊。"},
]

# Request bodies are declared by hand: they are small, and the code that accepts them also validates
# values (an inferred schema would miss the enums that make the body useful).
ENDPOINTS = [
    {
        "path": "/terraform/v1/ffmpeg/forward/streams",
        "tag": "forward",
        "summary": "列出轉播目標與即時狀態",
        "description": "回傳每個目標的狀態、指標、事件時間線與近期日誌。`stream` 是目標自己的輸出串流"
                       "（本機目標會顯示，外部平台不含 secret）。",
        "body": {"type": "object", "properties": {
            "token": {"type": "string", "description": "API token（也可改用 Authorization: Bearer）。"},
            "action": {"type": "string", "enum": ["query"], "description": "預設 query。"},
        }},
        "capture": "forward-streams",
    },
    {
        "path": "/terraform/v1/ffmpeg/forward/secret",
        "tag": "forward",
        "summary": "查詢／修改轉播目標",
        "description": "省略 action 等同查詢全部目標。`metrics`／`sessions`／`timing` 只讀，需要 `platform`；"
                       "其餘動作需要完整目標設定。",
        "actions": [
            ("（省略）", "查詢全部目標設定，回傳 platform → 設定的物件。"),
            ("update", "新增或更新目標；platform 為自訂目標時 key 由 label 推導（forwarding-<slug>）。"),
            ("delete", "刪除目標，同時清掉它的指標與執行紀錄，並踢除 SRS 發佈者。"),
            ("toggle", "切換 enabled；不影響其他目標。"),
            ("end", "手動結束直播：composite 顯示結束畫面並在寬限後停止，hold 立即停止。"),
            ("cancel-end", "取消結束直播，恢復目標。"),
            ("metrics", "本次執行的取樣歷史（5 秒一筆，最多 720 筆）。"),
            ("sessions", "最近 10 場執行紀錄（含進行中的那一場）。"),
            ("timing", "來源時間軸側錄清單（每個 platform 保留最近 5 次）。"),
        ],
        "body": {"type": "object", "properties": {
            "token": {"type": "string"},
            "action": {"type": "string", "enum": [
                "update", "delete", "toggle", "end", "cancel-end", "metrics", "sessions", "timing"]},
            "platform": {"type": "string", "description": "目標 key；自訂目標為 forwarding-<slug>。"},
            "stream": {"type": "string", "description": "來源串流（留空表示自動選擇）。"},
            "server": {"type": "string", "description": "rtmp(s)://host/app/ 形式的伺服器位址。"},
            "secret": {"type": "string", "description": "目標的串流金鑰；本機目標可帶 ?secret=。"},
            "enabled": {"type": "boolean"},
            "custom": {"type": "boolean", "description": "內建平台不可設為 true。"},
            "label": {"type": "string", "description": "對外顯示名稱；必須與 platform 一致（forwarding-<slug>）。"},
            "failover": {"type": "string", "enum": ["hold", "slate"],
                         "description": "hold 凍結最後一格（零空窗）；slate 顯示備援畫面（會重新編碼）。"},
            "normalize": {"type": "boolean", "description": "已由全域設定取代，保留相容。"},
            "ended": {"type": "boolean"},
            "endedAt": {"type": "integer", "format": "int64"},
        }},
        "capture": "forward-secret",
    },
    {
        "path": "/terraform/v1/mgmt/streams/query",
        "tag": "streams",
        "summary": "查詢 SRS 串流與生命週期事件",
        "description": "帶 vhost/app/stream 可精確查單路；不帶則回傳全部。events 是發布／取消／踢除紀錄"
                       "（播放事件另存）。",
        "body": {"type": "object", "properties": {
            "token": {"type": "string"},
            "vhost": {"type": "string"}, "app": {"type": "string"}, "stream": {"type": "string"},
        }},
        "capture": "streams-query",
    },
    {
        "path": "/terraform/v1/mgmt/record/query",
        "tag": "record",
        "summary": "查詢錄影狀態",
        "description": "回傳進行中與已完成的錄影，以及判定為異常的原因。錄影由 SRS 的 on_hls 事件驅動，"
                       "HLS 關閉時不會產生檔案。",
        "body": {"type": "object", "properties": {"token": {"type": "string"}}},
        "capture": "record-query",
    },
    {
        "path": "/terraform/v1/mgmt/disk/query",
        "tag": "system",
        "summary": "查詢磁碟容量",
        "description": "回報平台會寫入的檔案系統；overlay 這類虛擬磁碟的可用空間會被夾到宿主實際可用值，"
                       "並以 virtual 標記。",
        "body": {"type": "object", "properties": {"token": {"type": "string"}}},
        "capture": "disk-query",
    },
    {
        "path": "/terraform/v1/mgmt/gpu/query",
        "tag": "system",
        "summary": "查詢 GPU 與編碼器狀態",
        "description": "沒有掛載 GPU 時 available=false。WSL 不回報編碼器／解碼器利用率，該值為 0 代表未知。",
        "body": {"type": "object", "properties": {"token": {"type": "string"}}},
        "capture": "gpu-query",
    },
    {
        "path": "/terraform/v1/mgmt/srs/health/query",
        "tag": "system",
        "summary": "查詢 SRS 日誌健康",
        "description": "掃描 /data/logs/srs.log 最近 256KB，統計錯誤、警告與四類關鍵失敗"
                       "（publish timeout、kickoff、queue overflow、時間戳異常），並把相似訊息歸群。",
        "body": {"type": "object", "properties": {"token": {"type": "string"}}},
        "capture": "srs-health",
    },
    {
        "path": "/terraform/v1/mgmt/versions",
        "tag": "system",
        "summary": "查詢版本",
        "description": "回傳平台版本與 SRS 核心版本／來源（upstream 或 local）。",
        "body": {"type": "object", "properties": {"token": {"type": "string"}}},
        "capture": "versions",
    },
]


NULL = {"__null__": True}


def infer(value, depth=0):
    """Infer a JSON-schema fragment from a captured value."""
    if depth > 8:
        return {}
    if value is None:
        return dict(NULL)
    if isinstance(value, bool):
        return {"type": "boolean"}
    if isinstance(value, (int, float)):
        return {"type": "number"}
    if isinstance(value, str):
        return {"type": "string"}
    if isinstance(value, list):
        items = [infer(v, depth + 1) for v in value[:3] if v is not None]
        merged = merge_all(items)
        return {"type": "array", "items": merged} if merged else {"type": "array", "items": {}}
    props = {k: infer(v, depth + 1) for k, v in value.items()}
    return {"type": "object", "properties": props}


def merge_all(schemas):
    if not schemas:
        return {}
    out = dict(schemas[0])
    for s in schemas[1:]:
        out = merge(out, s)
    return out


def merge(a, b):
    if a.get("__null__") or b.get("__null__"):
        other = b if a.get("__null__") else a
        if other.get("__null__"):
            return dict(NULL)
        out = clean(other)
        out["nullable"] = True
        return out
    if a.get("type") != b.get("type"):
        # A field can be absent or differently typed between samples; keep both truths visible.
        types = [t for t in (a.get("type"), b.get("type")) if t]
        out: dict = {}
        if types:
            out = {"oneOf": [{"type": t} for t in dict.fromkeys(types)]}
        if a.get("nullable") or b.get("nullable"):
            out["nullable"] = True
        return out
    if a.get("type") == "object":
        props = dict(a.get("properties", {}))
        for k, v in b.get("properties", {}).items():
            props[k] = merge(props[k], v) if k in props else v
        return {"type": "object", "properties": props}
    if a.get("type") == "array":
        items = merge(a.get("items", {}), b.get("items", {}))
        return {"type": "array", "items": items}
    return a


def clean(schema):
    """Remove the internal null sentinel; OAS only allows nullable next to a type."""
    if not isinstance(schema, dict):
        return schema
    out = {}
    for k, v in schema.items():
        if k == "__null__":
            continue
        if k == "properties" and isinstance(v, dict):
            out[k] = {name: clean(sub) for name, sub in v.items()}
        elif k == "items":
            out[k] = clean(v)
        elif k == "oneOf":
            out[k] = [clean(x) for x in v]
        else:
            out[k] = v
    if out.get("nullable") and "type" not in out and "oneOf" not in out:
        out.pop("nullable", None)
    return out


def truncate(value, depth=0):
    """Keep examples readable: arrays lose their tail, long strings are cut."""
    if depth > 6:
        return value
    if isinstance(value, list):
        return [truncate(v, depth + 1) for v in value[:2]]
    if isinstance(value, dict):
        return {k: truncate(v, depth + 1) for k, v in value.items()}
    if isinstance(value, str) and len(value) > 300:
        return value[:300] + "…"
    return value


def load_capture(dirname, name):
    fp = os.path.join(dirname, name + ".json")
    if not os.path.exists(fp):
        raise SystemExit("missing capture: %s (run the query once and save the body)" % fp)
    return json.load(io.open(fp, encoding="utf-8"))


def build(captures):
    paths = {}
    for ep in ENDPOINTS:
        data = load_capture(captures, ep["capture"])
        payload = data.get("data", data)
        schema = clean(infer(payload))
        example = truncate(payload)

        op = {
            "operationId": ep["path"].replace("/terraform/v1/", "").replace("/", "_").replace("-", "_"),
            "tags": [ep["tag"]],
            "summary": ep["summary"],
            "description": ep["description"],
            "security": [{"bearerAuth": []}],
            "requestBody": {
                "required": False,
                "content": {"application/json": {
                    "schema": ep["body"],
                    "example": {"token": "<API token>", "action": ep.get("actions", [["", ""]])[0][0]}
                    if ep.get("actions") else {"token": "<API token>"},
                }},
            },
            "responses": {
                "200": {
                    "description": "成功；`code` 非 0 表示錯誤，`data` 的形狀見 schema。",
                    "content": {"application/json": {
                        "schema": {
                            "allOf": [
                                {"$ref": "#/components/schemas/ApiResponse"},
                                {"type": "object", "properties": {"data": schema}},
                            ]
                        },
                        "example": {"code": 0, "data": example, "server": "Oryx/%s" % platform_version()},
                    }},
                },
                "401": {"$ref": "#/components/responses/Unauthorized"},
            },
        }
        if ep.get("actions"):
            # Structured instead of a markdown table inside the description: the viewer renders it as
            # a table and plain OpenAPI consumers can still read it.
            op["x-actions"] = [{"action": a, "description": d} for a, d in ep["actions"]]

        paths[ep["path"]] = {"post": op}

    return {
        "openapi": "3.0.3",
        "info": {
            "title": "Oryx 維運 API",
            "version": platform_version(),
            "description": "平台維運端點的規格。完整端點清單見 docs/api-reference.md；"
                           "本規格只涵蓋實際會用到的部分，schema 由真實回應推導。",
        },
        "servers": [{"url": "http://localhost:882", "description": "本機預設埠"}],
        "tags": TAGS,
        "components": {
            "securitySchemes": {
                "bearerAuth": {"type": "http", "scheme": "bearer",
                               "description": "Authorization: Bearer <API token>；也接受 body 內的 token 欄位。"},
            },
            "responses": {
                "Unauthorized": {"description": "token 缺少或不正確。"},
            },
            "schemas": {
                "ApiResponse": {
                    "type": "object",
                    "properties": {
                        "code": {"type": "integer", "description": "0 表示成功。"},
                        "server": {"type": "string", "description": "Oryx/<version>"},
                    },
                    "required": ["code"],
                },
            },
        },
        "paths": paths,
    }


def platform_version():
    text = io.open(os.path.join(ROOT, "platform", "version.go"), encoding="utf-8").read()
    m = re.search(r'const version = "([^"]+)"', text)
    return m.group(1) if m else "unknown"


def documented_paths():
    if not os.path.exists(API_REF):
        return set()
    text = io.open(API_REF, encoding="utf-8").read()
    return set(re.findall(r"\| `(/terraform/v1/[^`]+)` \|", text))


def check():
    if not os.path.exists(OUT):
        print("openapi.json missing; run with --captures <dir>")
        return 1
    spec = json.load(io.open(OUT, encoding="utf-8"))
    known = documented_paths()
    bad = [p for p in spec["paths"] if p not in known]
    if bad:
        print("documented path(s) not found in docs/api-reference.md: %s" % ", ".join(bad))
        return 1
    print("openapi ok: %d paths, all present in api-reference.md" % len(spec["paths"]))
    return 0


def main():
    args = sys.argv[1:]
    if "--check" in args:
        return check()
    if "--captures" not in args:
        print(__doc__)
        return 0
    captures = args[args.index("--captures") + 1]
    spec = build(captures)
    text = json.dumps(spec, ensure_ascii=False, indent=2) + "\n"
    io.open(OUT, "w", encoding="utf-8", newline="\n").write(text)
    print("wrote %s (%d paths, version %s)" % (OUT, len(spec["paths"]), spec["info"]["version"]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
