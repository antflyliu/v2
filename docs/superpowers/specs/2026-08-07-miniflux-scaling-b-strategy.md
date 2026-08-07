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

# 代理：生产双层 —— 全局轮换（下）+ 问题域/P0 用 feed.proxy_url（上）
HTTP_CLIENT_PROXIES=http://u:p@res1:port,http://u:p@res2:port,http://u:p@res3:port
# HTTP_CLIENT_PROXY=   # 5000 源主路径不要依赖单全局代理

# CF bypass（已实现）
CLOUDFLARE_BYPASS_ENABLED=1
CLOUDFLARE_BYPASS_URL=http://127.0.0.1:5072
CLOUDFLARE_BYPASS_TIMEOUT=120
CLOUDFLARE_BYPASS_CACHE_TTL=25
```

说明：

- `HTTP_CLIENT_TIMEOUT` 建议 **45–90s**，避免单 feed 占死 worker 5 分钟（当前 300s 偏松，扩量后改为 90 更利于吞吐）。
- `BATCH_SIZE` 与 `POLLING_FREQUENCY` 共同决定调度吞吐；只加 worker 不提高 due 供给会空转。
- `WORKER_POOL_SIZE` **无代理池或池质量差时不要盲目 >10**；有稳定多出口后再 12–16。
- 代理细节见 §5.1（生产默认双层，非三级独立池）。

### 4.3 吞吐验算（阶段 3 示例）

- `POLLING_FREQUENCY=1`, `BATCH_SIZE=250` → ~15,000 次/小时  
- 5000 源平均约 **20 分钟**一轮（再经 entry_frequency：热更快、冷更慢）  
- 热源受 `MIN_INTERVAL=10~15` 约束，不会无限逼近 1 分钟。

---

## 5. 代理与 Cloudflare 容量

### 5.1 生产推荐：双层代理（全局轮换 + 问题域/P0 固定）

**决策（生产默认）：** 不做「P0/P1/P2 三套独立代理池」产品；用 Miniflux **现有**能力实现「又稳又快」：

| 层 | 机制 | 配置位置 | 谁用 |
|----|------|----------|------|
| L1 全局轮换池 | `HTTP_CLIENT_PROXIES` 逗号列表 → `ProxyRotator` round-robin | 环境变量 / `.env` | **默认**：P1 普通源、P2 冷源、未单独指定代理的源 |
| L2 单源固定出口 | Feed 字段 `proxy_url`（优先级最高） | 订阅源编辑 / API | **P0 热源**、**已知易 429/CF 域名**（同域多 feed **共用同一 URL**） |
| （可选）L0 单全局代理 | `HTTP_CLIENT_PROXY` + feed `fetch_via_proxy` | env + feed | 仅适合「全家一个出口」的小规模；**5000 源不推荐作为主路径** |

解析顺序（已实现，含 sticky lock）：  
`feed.proxy_url` → (`fetch_via_proxy` + `HTTP_CLIENT_PROXY`) → `HTTP_CLIENT_PROXIES` 轮换 → 直连。

#### 为什么这是「又稳又快」的默认

| 目标 | 做法 |
|------|------|
| **稳（防 429）** | 多数流量走多出口轮换，避免单机房 IP 被集中限流；问题域不靠加 `WORKER` 硬刚 |
| **快（热源/CF）** | P0 与易 CF 域 **固定优质出口** → 同 host+proxy 的 clearance **缓存命中率高**，少打 Camoufox，体感更快 |
| **运维成本** | 不维护三套池；只维护「一份全局列表 + 少数 feed 的 proxy_url」 |
| **与现网一致** | 无需新配置项、无新 UI；扩量期即可落地 |

#### 明确不做（v1）

- 无 `HTTP_CLIENT_PROXIES_P0` / 多池 env。  
- 无「按域名自动粘滞选路」全局逻辑（仅有：单次请求 resolve 后 lock；以及人工同域同 `proxy_url`）。  
- 无「429 后自动换下一个代理再试同一逻辑请求」（失败靠 next_check 退避；换出口靠人工改 `proxy_url` 或等下次轮换）。

若未来 429 仍系统性爆炸，再单独立项：域名→代理映射或 429 换出口重试。

#### 生产配置示例

```env
# L1：全局轮换（P1/P2 默认）。住宅/ISP 优先于廉价机房。
# 阶段 0–1：3～5 条；阶段 2–3：8～20 条量级（按预算与 429 情况加）
HTTP_CLIENT_PROXIES=http://u:p@res1:port,http://u:p@res2:port,http://u:p@res3:port

# 一般不要再设 HTTP_CLIENT_PROXY 与池「抢语义」；主路径用 PROXIES 即可。
# HTTP_CLIENT_PROXY=

WORKER_POOL_SIZE=10   # 有稳定池后再 12–16；无池或池很差时 ≤8
```

Feed 侧（运营）：

| 场景 | 配置 |
|------|------|
| 多个 `linux.do` / 同站多 feed | **相同** `proxy_url`（同一优质出口） |
| P0 热源且源站敏感 | 独立或共享优质 `proxy_url`；勿与劣质轮换混用 |
| 普通 RSS（无 CF、少 429） | 不填 `proxy_url`，吃 `HTTP_CLIENT_PROXIES` |
| 冷源、公开 CDN | 可不走代理（不填 + 若轮换会命中池则仍走池；若需直连需保证未进强制代理路径） |

#### 容量与阶段

| 阶段 | 源数量 | 全局池规模（量级） | Worker | 备注 |
|------|--------|--------------------|--------|------|
| 0 | ~66 | 1～3 出口即可验证 | 6–8 | 先固定 linux.do 等 `proxy_url` 消 429 |
| 1 | ~500 | 3～5 | 8–10 | 池质量 > 数量 |
| 2 | ~2000 | 5～10 | 10–12 | 观察 429 再加出口，勿只加 worker |
| 3 | ~5000 | 8～20 | 12–16 | 出口不够时优先加代理，不优先加到 32 worker |

原则：

- 业务请求与 clearance solve **同一 proxy**（sticky，已实现）。  
- 缓存 key = host + redacted(proxy) → **同域同 proxy_url 才能共享 clearance**。  
- 429 时：**降该域压力 / 换该域固定出口 / 降 worker**，而不是无限加大 `WORKER_POOL_SIZE`。  
- 代理质量：易 429 与 CF 站优先 **住宅或 ISP**；纯机房 IP 只给低风险 P1/P2。

### 5.2 camoufox-turnstile

| 项 | 建议 |
|----|------|
| `max_browsers` | 1（阶段 0–1）；CF 源占比高或 solve 排队明显时 2 |
| `queue_size` | ≥ 32 |
| `solve_timeout_sec` | ≥ Miniflux `CLOUDFLARE_BYPASS_TIMEOUT` |
| `proxy`（config.json） | 仅作任务未带 proxy 时的 fallback；生产以 Miniflux sticky 为准 |
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

1. ~~代理形态~~ → **已定**：双层（`HTTP_CLIENT_PROXIES` + P0/问题域 `proxy_url`）。待确认的是**你现有出口数量与类型**（住宅/机房）。  
2. 5000 源中预估 CF 挑战占比？  
3. P0 热源预计数量级（几十 / 几百）？  
4. 是否接受将 `HTTP_CLIENT_TIMEOUT` 从 300 降到 90？

---

## 11. 修订记录

| 日期 | 变更 |
|------|------|
| 2026-08-07 | 初稿：策略 B 分层实时配置设计（用户选定 B） |
| 2026-08-07 | §5.1 定为生产双层代理；否定 v1 多级独立代理池 |
