# routers-console 监控：Redis / API tab

> 用途：SRS 控制台（routers-console）只看得见 SRS 自己。平台自身的 Redis 状态、以及平台 HTTP 接口的延迟/超时，SRS 无从得知——本文件说明 2026-09 新增的「Redis / API」tab 怎么把这两块补上。

## 1. 为什么需要

- SRS 的 `/api/v1/summaries` 只报 SRS 进程/系统，不含平台侧 Redis。
- 平台所有状态都放在容器内的 Redis；endpoint 变慢/超时往往是 Redis 或 SRS 负载变高的**表象**。把 Redis 负载与 HTTP 接口延迟放同一页，才能定位「卡在 Redis、SRS 还是平台 handler 本身」。
- 参考数据（本机实测，Redis 5.0.14）：`keyspace_hits:38320 / misses:274644`，命中率仅约 12%——大量「读不存在的 key」，这类指标有助于日后从根上修掉这类查询模式。

## 2. 后端

代码在 `platform/console-metrics.go`（单测 `console-metrics_test.go`），middleware 挂在 `httpService.Run` 的 `serviceHandler` 外层（`consoleMetrics.Wrap(...)`），因此 mgmt API、SRS proxy、静态页全部被计时；两支 endpoint 在 `handleHTTPService` 注册，都走 `middlewareAuthTokenInBody`（UI 带 Bearer）。

`statusRecorder` 只覆写 `WriteHeader` 记录状态码（不覆写 `Write`，避免给 CodeQL 一个 `go/reflected-xss` 输出点）；它同时实作 `Unwrap()` 与 `Flush()`，让被包住的 `http.ResponseWriter` 的选配介面（`http.Flusher` 等）仍可达。否则 `/live/*.flv` 反向代理无法 flush，长连线 FLV 会被缓冲而非串流。回归测试 `TestConsoleMetricsFlushPassthrough`。

### 2.1 `/terraform/v1/mgmt/redis/info`

`rdb.Info()`（默认 sections）解析为 typed snapshot：

| 栏位群 | 内容 |
|---|---|
| server | `redis_version` / `role` / `uptime_sec` |
| clients | `connected_clients` / `blocked_clients` / `rejected_connections` |
| memory | `used_memory` / `used_memory_rss` / `used_memory_peak` / `maxmemory` / `maxmemory_policy` / `mem_fragmentation_ratio` |
| stats | `total_commands_processed` / `instantaneous_ops_per_sec` / `total_net_input_bytes` / `total_net_output_bytes` / `expired_keys` / `evicted_keys` / `keyspace_hits` / `keyspace_misses` |
| persistence | `rdb_last_bgsave_status` / `rdb_changes_since_last_save` / `aof_enabled` |
| keyspace | db0 的 `keys` / `expires` |

注意：
- INFO 的 section 名（`# Server` 等）parse 时统一转**小写**。
- **所有计数器都是绝对值**，客户端用相邻两次 poll 的差值算 rate（`calcRedisRates`），不会重复计算。
- `keyspace` 只取 `db0`（平台只用一个 db）。

### 2.2 `/terraform/v1/mgmt/http/metrics`

`consoleMetricsStore`：60 秒滑动窗，每 route 存采样（时间 + 微秒 + 是否 5xx），总采样上限 10 万笔，超限丢弃最旧 route。回传聚合结果（按 max_ms 降序）：

```
window_sec, routes: [{method, route, count, rate_per_sec, avg_ms, max_ms, p95_ms, error_count, slow_count}]
```

记录规则（`normalizeConsoleRoute`）：
- 跳过：`OPTIONS`、metrics endpoint 自身的轮询。
- `/players/*`、`/tools/*` 各归一族（静态档，避免每档一条）。
- `.flv` / `.m3u8` / `.ts` / `.aac` / `.mp3` 是**长连接或媒体大流量**，其「handler 时长」等价于整段推流时长，会污染 slow 列表，故整类**不记录**。
- `/terraform/` 固定路由保留原路径（mgmt API 各自一条）。
- 其余（`/api/v1/*`、`/rtc/*`）收成 family：第 4 段之后收敛为 `/{id}`（如 `/api/v1/clients/{id}`）。

设计取舍：`slow_count` 以 >1000ms 计；`rate_per_sec` 以固定 60 秒窗口为分母（不用首尾采样跨距），窗口内少见 route 的 RPS 不会被放大。

## 3. 前端

`ui/src/pages/SrsConsole.js` → `SrsRedisMetrics`（tab eventKey `redis`，标题键 `console.redisTab`）。

- **轮询**：每 3 秒，但仅在 tab 启用（`active` prop）且 `document.visibilityState === 'visible'` 时执行——切走 tab 或页面隐藏即暂停。
- **资料卡三张**：Redis 服务器（版本/角色/运行时间/Keys/过期/持久化）、内存（used/peak/rss/frag ratio/淘汰策略/evicted）、流量与效率（ops/s、指令速率、网络 in/out、命中率、connected/blocked/rejected）。
- **趋势图**：recharts，四张线/面积图——ops/s、memory+RSS(MB)、命中率(%)、clients；series 保留 240 点（3s × 240 ≈ 12 分钟）。命中率用相邻两次 poll 的 delta 计算（`calcRedisRates`）。
- **HTTP 接口延迟表**：上方 endpoint 回传的 route 列表，前 120 条；5xx 与 >1s 用 Badge 标红。
- i18n：key 都挂在 `console` 下（`console.redis*` / `console.api*`），en/ja/zh 三语系同步新增。
- 首次引入 recharts（`recharts ^2.1.8` 原本就是依赖但一直没人 import，现在真正进入 bundle）。
- 一并把 `BrowserRouter` 的 v7 future flag 打开（`v7_startTransition` / `v7_relativeSplatPath`，react-router 6.30 对 React 17 有 startTransition shim，安全），消除 console 里两条 React Router 警告。

## 4. 测试与验证

安全说明：2026-09 的 CodeQL 曾将 `statusRecorder.Write()` 标为反射型 XSS 输出点。程序检查发现，共用 HTTP 函式库原本会将未经验证的 JSONP `callback` 拼入 JavaScript；后续已移除共用 writer 的 JSONP 支持，忽略 callback 并固定回传 JSON，加入 nosniff。2026-09-11 进一步做设计根治：`statusRecorder` 只需要记录状态码，`Write()` override 本身多余（`Wrap()` 已把状态 0 视为 200），且正是被标记的 sink，故直接移除该 override，让嵌入的 `ResponseWriter` 提供 `Write`，sink 在自有程式中消失。`console-metrics_test.go` 覆盖经过 middleware 的攻击、正常 JSON 与错误回应案例。CodeQL 尚待重扫确认。完整成因、修正范围及验证状态见 [CodeQL 安全漏洞修复说明](security-fix-codeql.md)。

- 后端：在 `platform/` 目录、`golang:1.26` 容器内执行 `go test -mod=vendor . -run 'TestConsoleMetrics|TestNormalizeConsoleRoute|TestBuildRedisInfoSnapshot' -count=1`，已通过，包含 JSONP 移除后的安全回归案例。
- 前端：`npx vitest run`（含 `SrsConsole.test.js` 的 `calcRedisRates` 单测）
- 监控页面变更需重新 build UI 并重建 Docker 映像；本次 JSONP 修正位于后端 vendored 函式库，需以 `-mod=vendor` 重建后端并更新容器，仅更新 UI 不会生效。目前尚未部署此修正，也尚未重跑 CodeQL。

## Valkey 相容性

现行迁移方向见 [Redis 5 直接迁移 Valkey](valkey-migration.md)。INFO 回应新增 server_name / server_version，保留 redis_version 相容字段；Valkey 的版本显示使用真实 valkey_version，不能把 Redis 相容版本当成服务版本。
