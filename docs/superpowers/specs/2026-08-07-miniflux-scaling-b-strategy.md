# Miniflux 5000 源扩容 — 策略 B「分层实时」配置设计

**Date:** 2026-08-07  
**Status:** Draft (awaiting user review)  
**Scope:** 配置与运营策略（不改 Miniflux 核心代码；不引入新服务）  
**Related:** Cloudflare clearance bypass (`docs/superpowers/specs/2026-08-06-cloudflare-clearance-bypass-design.md`)

---

## 1. 目标与约束

### 1.1 目标

| 优先级 | 目标 |
|--------|------|
| P0 | 系统稳定；频繁采集时尽量不被源站 429 / 拒绝 |
| P1 | 在稳定前提下尽可能实时（热源 10–20 分钟级） |
| P2 | 支持从当前 ~66 源平滑扩到 ~5000 源 |

### 1.2 硬约束

- **单实例 Miniflux**（当前部署形态）。
- 调度器：`entry_frequency`（热源勤、冷源稀）。
- 已接入 **Cloudflare clearance bypass**（camoufox-turnstile + 进程内 clearance 缓存）。
- Sticky proxy：solve 与业务重试同一出口。
- **不**对全部 5000 源追求 5 分钟全员刷新（吞吐与 429 均不可持续）。

### 1.3 吞吐近似公式

每小时最多刷新次数 ≈ `(60 / POLLING_FREQUENCY) × BATCH_SIZE`

Worker 并发上限 = `WORKER_POOL_SIZE`（同时 in-flight 请求数）。

429 主要来自 **同域名过密**，不是全局 feed 总数本身。

---

## 2. 策略选择

在 A（稳态优先）/ B（分层实时）/ C（激进实时）中，选定：

**策略 B — 分层实时**

- 全局：`entry_frequency` + 适中调度吞吐。
- 热源：更短有效间隔（靠 min interval + 源级运营）。
- 冷源：拉长间隔、克制 crawler。
- 防 429：代理池 + 同域预算 + CF 容量控制。

---

## 3. 源分层模型

### 3.1 三层定义

| 层级 | 名称 | 定义（运营规则） | 目标检查间隔 |
|------|------|------------------|--------------|
| P0 | 热源 | 业务关键、更新频繁、需要尽量实时；人工标记或「近 7 日高产」 | 10–20 分钟 |
| P1 | 普通 | 默认订阅源 | 30–120 分钟（由 entry_frequency 自适应） |
| P2 | 冷源 | 更新极少、或可容忍延迟；近 7 日几乎无新条目 | 6–12 小时 |

### 3.2 分层手段（v1，配置/运营，不改代码）

Miniflux 上游 **没有** 原生「热源标签 → 独立调度队列」。v1 用现有能力近似：

1. **全局** `entry_frequency`：按周条目量自动疏密（核心）。
2. **`SCHEDULER_ENTRY_FREQUENCY_MIN_INTERVAL`**：全员最快下限 → 控制热源上限速度，也防止全体过密。
3. **`SCHEDULER_ENTRY_FREQUENCY_MAX_INTERVAL`**：冷源上限。
4. **Feed 级运营**：
   - 同站多 feed：错开手动刷新；必要时合并或关掉重复源。
   - 全文抓取 `crawler`：仅 P0/确需全文的源开启。
   - 可选：P2 源 `disabled` 轮换或更长检查（依赖调度结果 + 减少强制刷新）。
5. **代理**：P0 与易 429 域名优先固定优质出口（sticky + 多 proxy 列表由现有 proxy/rotator 能力承担）。

> 若后续需要「真正独立的 P0 队列」，属于产品增强，不在本配置设计范围内。

### 3.3 同域预算规则

对同一注册域名（例：`linux.do`）：

- 并发：建议 **同时 in-flight ≤ 1–2**（靠控制 worker 总数 + 避免一键「刷新全部」）。
- 刷新：避免多个同域 feed 在同一分钟被手动连点刷新。
- 429 后：信任 Miniflux 对 `Retry-After` / rate-limit 的 `ScheduleNextCheck` 退避，**不要立刻连刷**。

---

## 4. 全局配置参数

### 4.1 阶段表（主路径）

| 阶段 | 源数量 | POLLING_FREQUENCY (min) | BATCH_SIZE | WORKER_POOL_SIZE | MIN_INTERVAL (min) | MAX_INTERVAL (min) | 说明 |
|------|--------|-------------------------|------------|------------------|--------------------|--------------------|------|
| 0 当前 | ~66 | 2 | 80 | 6–8 | 15 | 720 | 压住 429，验证 CF |
| 1 | ~500 | 2 | 100–150 | 8–10 | 12–15 | 720 | 引入/扩大代理池 |
| 2 | ~2000 | 1–2 | 150–200 | 10–12 | 10–15 | 720 | 抬吞吐 |
| 3 | ~5000 | 1 | 200–300 | 12–16 | 10–15 | 720 | 需稳定代理；CF 源多则加 browser 槽 |

### 4.2 推荐 `.env` 键（与 Miniflux 一致）

```env
POLLING_SCHEDULER=entry_frequency
POLLING_FREQUENCY=2
BATCH_SIZE=150
WORKER_POOL_SIZE=10
SCHEDULER_ENTRY_FREQUENCY_MIN_INTERVAL=15
SCHEDULER_ENTRY_FREQUENCY_MAX_INTERVAL=720
# SCHEDULER_ENTRY_FREQUENCY_FACTOR=  # 默认即可；发帖少的源更稀

HTTP_CLIENT_TIMEOUT=90

# CF bypass（已实现）
CLOUDFLARE_BYPASS_ENABLED=1
CLOUDFLARE_BYPASS_URL=http://127.0.0.1:5072
CLOUDFLARE_BYPASS_TIMEOUT=120
CLOUDFLARE_BYPASS_CACHE_TTL=25
```

说明：

- `HTTP_CLIENT_TIMEOUT` 建议 **45–90s**，避免单 feed 占死 worker 5 分钟（当前 300s 偏松，扩量后改为 90 更利于吞吐）。
- `BATCH_SIZE` 与 `POLLING_FREQUENCY` 共同决定调度吞吐；只加 worker 不提高 due 供给会空转。
- `WORKER_POOL_SIZE` **无代理池时不要盲目 >10**，同域 429 风险上升。

### 4.3 吞吐验算（阶段 3 示例）

- `POLLING_FREQUENCY=1`, `BATCH_SIZE=250` → ~15,000 次/小时  
- 5000 源平均约 **20 分钟**一轮（再经 entry_frequency：热更快、冷更慢）  
- 热源受 `MIN_INTERVAL=10~15` 约束，不会无限逼近 1 分钟。

---

## 5. 代理与 Cloudflare 容量

### 5.1 代理

| 阶段 | 建议 |
|------|------|
| 0–1 | 至少可用出口；同域 sticky |
| 2–3 | 多出口池（住宅/ISP 优先于机房 IP）；按域名分散 |

原则：

- 业务请求与 clearance solve **同一 proxy**（已实现 sticky）。
- 缓存 key = host + redacted proxy → 不同出口各自 clearance。
- 429 时优先换出口或拉长该域间隔，而不是加大全局 worker。

### 5.2 camoufox-turnstile

| 项 | 建议 |
|----|------|
| `max_browsers` | 1（阶段 0–1）；CF 源占比高时 2 |
| `queue_size` | ≥ 32 |
| `solve_timeout_sec` | ≥ Miniflux `CLOUDFLARE_BYPASS_TIMEOUT` |
| `headless` | 先 true；失败再按 ops 文档排查 |

Clearance **不写** `feeds.cookie`；进程内缓存 TTL 默认 25m。

---

## 6. 运营手册（防 429）

1. **禁止**在扩量期频繁点「后台刷新所有订阅源」。  
2. 同站多 feed（如多个 linux.do）：合并或错峰；crawler 只开必要的。  
3. 出现 429 文案（过多请求）时：等待 Next check；检查该域 feed 数量与 worker。  
4. 新增源默认 P1；确认需要实时再升 P0（运营标记 + 避免同域扎堆）。  
5. 扩量节奏：66 → 500 → 2000 → 5000，每阶段观察 429 与 CF solve 成功率再抬参。

---

## 7. 监控与成功标准

### 7.1 指标

| 指标 | 观察方式 | 目标（阶段 3） |
|------|----------|----------------|
| 429 / rate-limit 错误 | feed 错误文案 + 日志 | 持续高压下仍可控；同域不连环失败 |
| 热源检查延迟 | UI「下次检查」/ DB next_check | P0 多数 ≤ 20 分钟 |
| 普通源延迟 | 同上 | 多数 ≤ 2 小时 |
| CF `solve_ok` / `solve_err` | slog `component=cloudflare_bypass` | solve 失败不拖垮全局 worker |
| Worker 占用 | 超时/hang 日志 | 无大面积卡死在 300s 超时 |
| 调度积压 | due feed 是否长期 > BATCH | 积压则升 BATCH 或降平均间隔需求 |

### 7.2 成功标准（B 策略验收）

- [ ] 5000 源规模下进程可长期运行（无因采集打满导致不可用）。  
- [ ] P0 源体感接近 10–20 分钟级更新（在源站未 429 前提下）。  
- [ ] 同域 429 可通过退避 + 代理 + 降并发收敛，而不是靠无限重试。  
- [ ] CF 源在 bypass 开启时能自动 clearance 重试（见既有 smoke 文档）。

---

## 8. 非目标

- 不实现新的「热源调度队列」代码（除非另开需求）。  
- 不引入分布式多 Miniflux 分片（策略 C / 后续架构）。  
- 不保证全员 5 分钟刷新。  
- 不替代源站侧 rate limit；只能降低触发概率并正确退避。

---

## 9. 与当前环境的落点

| 文件/组件 | 作用 |
|-----------|------|
| `.env.dev`（本地，gitignore） | 放阶段 0/1 参数 + CF bypass |
| `dev++.ps1` | 加载 `.env.dev` 启动 |
| camoufox-turnstile `config.json` | browser 槽位与 solve 超时 |
| `docs/superpowers/ops/cloudflare-bypass-smoke.md` | CF 本地验收 |

当前已知问题：同域（linux.do）多 feed 429 — 优先用 **同域预算 + 降并发/错峰**，而不是只加 `WORKER_POOL_SIZE`。

---

## 10. 开放问题（实现前可确认）

1. 代理池现状：是否已有可轮换出口？数量级？  
2. 5000 源中预估 CF 挑战占比？  
3. P0 热源预计数量级（几十 / 几百）？  
4. 是否接受将 `HTTP_CLIENT_TIMEOUT` 从 300 降到 90？

---

## 11. 修订记录

| 日期 | 变更 |
|------|------|
| 2026-08-07 | 初稿：策略 B 分层实时配置设计（用户选定 B） |
