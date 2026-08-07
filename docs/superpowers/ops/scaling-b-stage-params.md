# 策略 B — 阶段参数表

与 spec `2026-08-07-miniflux-scaling-b-strategy.md` §4.1 / §5.1 对齐。  
改生产前先改本表并评审，再改 env。

## 调度

| 阶段 | 源数量 | POLLING_FREQUENCY (min) | BATCH_SIZE | WORKER_POOL_SIZE | MIN_INTERVAL | MAX_INTERVAL | HTTP_CLIENT_TIMEOUT (s) |
|------|--------|-------------------------|------------|------------------|--------------|--------------|-------------------------|
| 0 | ~66 | 2 | 80 | 6–8 | 15 | 720 | 90 |
| 1 | ~500 | 2 | 100–150 | 8–10 | 12–15 | 720 | 90 |
| 2 | ~2000 | 1–2 | 150–200 | 10–12 | 10–15 | 720 | 90 |
| 3 | ~5000 | 1 | 200–300 | 12–16 | 10–15 | 720 | 90 |

始终：

```env
POLLING_SCHEDULER=entry_frequency
```

吞吐近似：每小时刷新上限 ≈ `(60 / POLLING_FREQUENCY) × BATCH_SIZE`。

## 代理池规模（量级）

| 阶段 | HTTP_CLIENT_PROXIES 条数 | 备注 |
|------|--------------------------|------|
| 0 | 1–3 | 问题域必须先 `proxy_url` |
| 1 | 3–5 | 质量 > 数量 |
| 2 | 5–10 | 429 仍高则加出口不加工人 |
| 3 | 8–20 | 住宅/ISP 优先给敏感域 |

## CF / Camoufox

| 阶段 | max_browsers | CLOUDFLARE_BYPASS_TIMEOUT | CACHE_TTL |
|------|--------------|---------------------------|-----------|
| 0–1 | 1 | 120 | 25 |
| 2–3 CF 多 | 1–2 | 120 | 25 |

## 当前仓库本地 `.env.dev` 目标（阶段 0）

在 **不提交** 的前提下，本地应对齐：

```env
POLLING_SCHEDULER=entry_frequency
POLLING_FREQUENCY=2
BATCH_SIZE=80
WORKER_POOL_SIZE=8
SCHEDULER_ENTRY_FREQUENCY_MIN_INTERVAL=15
SCHEDULER_ENTRY_FREQUENCY_MAX_INTERVAL=720
HTTP_CLIENT_TIMEOUT=90
# HTTP_CLIENT_PROXIES=...  # 填真实出口，勿提交
# 已有 CF 块保持 ENABLED=1 与 URL
```
