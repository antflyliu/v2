# 策略 B 运维 Runbook — 分层实时 + 双层代理

Spec: `docs/superpowers/specs/2026-08-07-miniflux-scaling-b-strategy.md`  
阶段参数表: `docs/superpowers/ops/scaling-b-stage-params.md`  
CF smoke: `docs/superpowers/ops/cloudflare-bypass-smoke.md`

## 1. 源分层（运营，非三套调度队列）

| 层级 | 含义 | 代理 | crawler | 备注 |
|------|------|------|---------|------|
| P0 | 要尽量实时 | 优先固定 `proxy_url`（优质出口） | 仅必要时开 | 数量宜少 |
| P1 | 默认 | 吃 `HTTP_CLIENT_PROXIES` 轮换 | 默认关/按需 | 大多数源 |
| P2 | 可慢 | 全局池或低成本出口 | 建议关 | 拉长间隔靠 entry_frequency |

Miniflux **没有** P0 独立队列；靠 `entry_frequency` + `MIN_INTERVAL`/`MAX_INTERVAL` + 人工 `proxy_url`。

## 2. 双层代理

### 2.1 全局轮换（L1）

环境变量（`.env.dev` 或生产 env）：

```env
HTTP_CLIENT_PROXIES=http://u:p@host1:port,http://u:p@host2:port,http://u:p@host3:port
```

- 启动日志应出现类似：`Initializing proxy rotation` + `proxies_count=N`（见 `internal/cli/cli.go`）。
- **不要**把密钥提交进 git。

### 2.2 固定出口（L2）

对 **同一注册域名** 的多个 feed（例：linux.do 下多条订阅）：

1. 选 **一条** 优质代理 URL。
2. 在每条相关 feed 的编辑页或 API 写入 **相同** 的 `proxy_url`。
3. 不要给同域 feed 各绑不同劣质 IP（CF clearance 缓存 key = host + proxy，分散会反复 solve）。

API 示例（需有效 token；占位）：

```bash
# PATCH 单 feed — 将 FEED_ID / TOKEN / PROXY 换成真实值
curl -s -X PUT "http://127.0.0.1:8080/v1/feeds/FEED_ID" \
  -H "X-Auth-Token: TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"proxy_url\":\"http://u:p@good-proxy:port\"}"
```

（若实例 API 字段名以 OpenAPI/实际 handler 为准；UI 编辑页「代理」字段等价。）

### 2.3 解析顺序（勿依赖错层）

`feed.proxy_url` → (`fetch_via_proxy` + `HTTP_CLIENT_PROXY`) → `HTTP_CLIENT_PROXIES` 轮换 → 直连。

单次请求内 CF solve+retry 使用 **locked** 同一 proxy（已实现）。

## 3. 调度与超时（阶段 0 默认）

```env
POLLING_SCHEDULER=entry_frequency
POLLING_FREQUENCY=2
BATCH_SIZE=80
WORKER_POOL_SIZE=8
SCHEDULER_ENTRY_FREQUENCY_MIN_INTERVAL=15
SCHEDULER_ENTRY_FREQUENCY_MAX_INTERVAL=720
HTTP_CLIENT_TIMEOUT=90
```

抬参必须对照 `scaling-b-stage-params.md`，**先加代理质量/数量，再加 WORKER**。

## 4. 429 / 「过多请求」处置

1. **不要**连点「后台刷新所有订阅源」。
2. 查看是否同域多 feed 同时失败 → 统一 `proxy_url` + 等待 Next check。
3. 降低 `WORKER_POOL_SIZE` 1～2 档观察。
4. 确认未对同域全开 `crawler`。
5. 信任 Miniflux 对 rate-limit 的 next_check 退避；硬刷会加重 429。

## 5. Cloudflare

- 见 `cloudflare-bypass-smoke.md`。
- 日志关键字：`component=cloudflare_bypass`，`event=challenge_detected|solve_ok|solve_err|retry_ok|retry_fail|cache_hit`。
- Solver 与 Miniflux sticky proxy 一致；勿指望 clearance 写入 `feeds.cookie`。

## 6. 验收清单（阶段 0）

- [ ] Miniflux 用新 env **重启**（`cloudflare.Default()` 与配置均进程级）。
- [ ] 启动日志：`proxies_count` 与预期一致（若配置了 PROXIES）。
- [ ] 同域问题 feed 已设相同 `proxy_url`。
- [ ] 自然调度 1～2 小时内 429 明显少于「刷新全部」时期。
- [ ] CF 源出现 challenge 时有 solve 日志（若启用 bypass）。
- [ ] `HTTP_CLIENT_TIMEOUT=90` 无大面积误杀（必要时单源再调）。

## 7. 扩量门禁

| 从 → 到 | 门禁 |
|---------|------|
| 66 → 500 | 429 可控；代理 ≥3；WORKER≤10 |
| 500 → 2000 | 池 5～10；P0 列表明确；CF solve 无持续排队爆炸 |
| 2000 → 5000 | 池 8～20；WORKER 12～16；有监控习惯后再抬 BATCH |
