# Miniflux 5000 源扩容 — 策略 B「分层实时」Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把已批准的策略 B（分层实时 + 双层代理）落到可执行的运维文档与阶段 0/1 本地配置，使当前 ~66 源先稳后快，并为扩到 ~5000 源提供参数阶梯与验收清单——**不写新的多级代理池产品代码**。

**Architecture:** 纯配置与运营。调度用现有 `entry_frequency`；出口用现有 `HTTP_CLIENT_PROXIES`（全局轮换）+ feed `proxy_url`（P0/易 429 域固定）；CF 继续用已实现的 clearance bypass。本计划产出：运维 runbook、阶段参数表文档、本地 `.env.dev` 阶段 0 应用步骤（gitignore，不提交密钥）。

**Tech Stack:** Miniflux env 配置、`.env.dev` + `dev++.ps1`、camoufox-turnstile、Postgres 中 feed 字段 `proxy_url` / `fetch_via_proxy` / `crawler` / `cloudflare_bypass`。

## Global Constraints

- Spec SSOT: `docs/superpowers/specs/2026-08-07-miniflux-scaling-b-strategy.md`（用户已 OK）。
- **禁止** v1 实现：多套独立代理池 env、域名自动粘滞选路、429 自动换出口重试产品代码。
- **双层代理（生产默认）：** `HTTP_CLIENT_PROXIES` 轮换 + 问题域/P0 的 `proxy_url`；同域多 feed **共用同一** `proxy_url`。
- 调度：`POLLING_SCHEDULER=entry_frequency`。
- 阶段 0 目标参数（spec §4.1）：`POLLING_FREQUENCY=2`，`BATCH_SIZE=80`，`WORKER_POOL_SIZE=6~8`，`MIN_INTERVAL=15`，`MAX_INTERVAL=720`，`HTTP_CLIENT_TIMEOUT=90`。
- CF：`CLOUDFLARE_BYPASS_ENABLED=1`，`CLOUDFLARE_BYPASS_URL` 指向本地 solver；clearance **不写** `feeds.cookie`。
- `.env.dev` 在 `.gitignore` 中——**永不** `git add .env.dev`；计划中的密钥用占位符。
- 不修改 Miniflux 上游核心业务逻辑，除非验收中发现配置无法表达的硬缺陷（需另开设计）。
- 语言：运维文档可用中文；提交信息用英文 conventional commits。

---

## File map

| Path | Responsibility |
|------|----------------|
| `docs/superpowers/ops/scaling-b-runbook.md` | 策略 B 日常运维：分层、代理、429、阶段抬参、验收 |
| `docs/superpowers/ops/scaling-b-stage-params.md` | 66→500→2000→5000 参数表（可打印对照） |
| `.env.dev` (local only) | 阶段 0 调度 + 代理占位 + 已有 CF |
| `docs/superpowers/ops/cloudflare-bypass-smoke.md` | 已有；本计划只交叉引用，不重复大改 |
| Feed DB / UI | 人工或 API 设置 `proxy_url`（不写代码） |

---

### Task 1: 运维 Runbook（策略 B）

**Files:**
- Create: `docs/superpowers/ops/scaling-b-runbook.md`

**Interfaces:**
- Consumes: spec §3 分层、§5.1 双层代理、§6 运营、§7 监控
- Produces: 操作者可按文档完成阶段 0 落地与 429 处置

- [ ] **Step 1: 创建 runbook 文件**

写入完整内容（实现时原样创建该文件）：

```markdown
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
```

- [ ] **Step 2: 目视检查**

确认文件存在且无 `TBD`/`TODO` 占位句。

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/ops/scaling-b-runbook.md
git commit -m "docs(ops): add scaling strategy B runbook"
```

---

### Task 2: 阶段参数对照表

**Files:**
- Create: `docs/superpowers/ops/scaling-b-stage-params.md`

**Interfaces:**
- Consumes: spec §4.1 阶段表、§5.1 代理容量表
- Produces: 运维改 env 时的唯一数字 SSOT（与 spec 一致）

- [ ] **Step 1: 创建阶段参数文件**

```markdown
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
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/ops/scaling-b-stage-params.md
git commit -m "docs(ops): add scaling B stage parameter table"
```

---

### Task 3: 本地阶段 0 应用到 `.env.dev`（不提交）

**Files:**
- Modify (local only): `.env.dev` — **gitignored**

**Interfaces:**
- Consumes: Task 2 阶段 0 数字；已有 CF 块
- Produces: 重启后进程使用阶段 0 调度参数

- [ ] **Step 1: 读取当前 `.env.dev`**

```bash
# Windows / git bash
cat .env.dev
```

记录现有 `POLLING_*`、`WORKER_*`、`HTTP_CLIENT_*`、`CLOUDFLARE_*`。

- [ ] **Step 2: 将调度块改为阶段 0（保留 DATABASE/BASE_URL/CF）**

目标片段（代理 URL 由操作者填；无代理则整行注释掉）：

```env
POLLING_SCHEDULER=entry_frequency
POLLING_FREQUENCY=2
BATCH_SIZE=80
WORKER_POOL_SIZE=8
SCHEDULER_ENTRY_FREQUENCY_MIN_INTERVAL=15
SCHEDULER_ENTRY_FREQUENCY_MAX_INTERVAL=720

HTTP_CLIENT_TIMEOUT=90
FETCHER_ALLOW_PRIVATE_NETWORKS=1

# 有出口时取消注释并填写（勿提交本文件）
# HTTP_CLIENT_PROXIES=http://user:pass@proxy1:port,http://user:pass@proxy2:port

CLOUDFLARE_BYPASS_ENABLED=1
CLOUDFLARE_BYPASS_URL=http://127.0.0.1:5072
CLOUDFLARE_BYPASS_TIMEOUT=120
CLOUDFLARE_BYPASS_CACHE_TTL=25
```

- [ ] **Step 3: 确认未被 git 跟踪**

```bash
git check-ignore -v .env.dev
# 期望：.gitignore 命中 .env.dev
git status --short .env.dev
# 期望：无输出或忽略，绝不能出现 staged
```

- [ ] **Step 4: 重启 Miniflux**

```powershell
# 停掉旧进程后：
cd D:\WORKSPACE\all-monitor-space\miniflux-v2
.\dev++.ps1
```

- [ ] **Step 5: 验证配置已加载**

在 debug 日志 / 行为上确认：

- 调度更勤（`POLLING_FREQUENCY=2` 后 due 更及时）。
- 若配置了 `HTTP_CLIENT_PROXIES`，启动有 `Initializing proxy rotation`。
- **无** `git commit` 包含 `.env.dev`。

本 Task **无代码 commit**（仅本地文件）。若误 `git add .env.dev`，立即 `git reset HEAD .env.dev` 并确认 gitignore。

---

### Task 4: 问题域 feed `proxy_url` 运营步骤（文档化 + 手工执行清单）

**Files:**
- Modify: `docs/superpowers/ops/scaling-b-runbook.md`（若 Task 1 已含 §2.2，本 Task 追加「当前实例清单」小节即可）

**Interfaces:**
- Consumes: 用户实例中 linux.do 等 feed 列表
- Produces: 可勾选的同域固定代理执行记录

- [ ] **Step 1: 在 runbook 追加「当前实例 — 问题域清单」模板**

追加到 `scaling-b-runbook.md` 文末：

```markdown
## 8. 当前实例问题域清单（填写后勿提交密钥）

| 域名 | Feed 名称/ID | 统一 proxy_url 已设置 | 日期 |
|------|--------------|----------------------|------|
| linux.do | （填写） | [ ] | |
| | | [ ] | |

操作顺序：

1. 准备 1 条优质代理 URL。
2. 列出该域所有 feed ID（UI 订阅源列表或 API `GET /v1/feeds`）。
3. 逐个设置相同 `proxy_url`。
4. 等待自然 next_check，**不要**立即「刷新全部」。
5. 1 小时后看错误是否从「过多请求」下降。
```

- [ ] **Step 2: Commit 文档（仍无真实 proxy 密钥）**

```bash
git add docs/superpowers/ops/scaling-b-runbook.md
git commit -m "docs(ops): add per-domain proxy_url checklist to scaling B runbook"
```

- [ ] **Step 3: 操作者手工执行清单（不由 CI 完成）**

在运行中的 Miniflux 上按表格勾选；本步无 git commit。

---

### Task 5: 交叉链接与 spec 状态

**Files:**
- Modify: `docs/superpowers/specs/2026-08-07-miniflux-scaling-b-strategy.md`
- Modify: `docs/superpowers/ops/cloudflare-bypass-smoke.md`（文首加一行 Related 链接即可）

- [ ] **Step 1: spec 状态改为 Approved，并链到 plan/ops**

在 spec 顶部 `Status: Draft` 改为：

```markdown
**Status:** Approved (2026-08-07)  
**Plan:** `docs/superpowers/plans/2026-08-07-miniflux-scaling-b-strategy.md`  
**Ops:** `docs/superpowers/ops/scaling-b-runbook.md`, `docs/superpowers/ops/scaling-b-stage-params.md`
```

- [ ] **Step 2: CF smoke 文首增加 Related**

在 `cloudflare-bypass-smoke.md` 标题下增加：

```markdown
Related scaling ops: `docs/superpowers/ops/scaling-b-runbook.md`
```

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-08-07-miniflux-scaling-b-strategy.md \
        docs/superpowers/ops/cloudflare-bypass-smoke.md
git commit -m "docs: mark scaling B spec approved and cross-link ops"
```

---

### Task 6: 验收（阶段 0 离线 + 操作者在线）

**Files:** 无强制代码变更

- [ ] **Step 1: 文档齐全**

```bash
test -f docs/superpowers/ops/scaling-b-runbook.md && echo OK_runbook
test -f docs/superpowers/ops/scaling-b-stage-params.md && echo OK_params
test -f docs/superpowers/plans/2026-08-07-miniflux-scaling-b-strategy.md && echo OK_plan
```

Expected: 三行 OK_。

- [ ] **Step 2: 确认无密钥进库**

```bash
git log -5 --oneline
git grep -n "HTTP_CLIENT_PROXIES=http" docs/ || true
# docs 中仅允许占位示例，禁止真实密码
```

- [ ] **Step 3: 配置相关单测（回归，非本策略新逻辑）**

```bash
go test ./internal/config/ -count=1 -skip 'TestParseAdminPasswordFileOptionWithEmptyFile'
```

Expected: `ok`。

- [ ] **Step 4: 操作者在线验收（手工）**

按 runbook §6 勾选；记录 429 是否缓解。  
失败时：回到 Task 3/4，检查是否仍「刷新全部」或未统一 `proxy_url`。

- [ ] **Step 5: 完成记录**

在 `docs/superpowers/ops/scaling-b-runbook.md` 可选追加：

```markdown
## 9. 阶段 0 验收记录

| 日期 | 结果 | 备注 |
|------|------|------|
| YYYY-MM-DD | PASS/FAIL | |
```

若追加了验收记录且无密钥：

```bash
git add docs/superpowers/ops/scaling-b-runbook.md
git commit -m "docs(ops): record scaling B stage-0 acceptance"
```

---

## Spec coverage checklist

| Spec 章节 | Task |
|-----------|------|
| §1 目标与约束 | Global Constraints + Task 6 |
| §2 策略 B | 全文 |
| §3 分层模型 | Task 1 §1 |
| §4 全局配置 / 阶段表 | Task 2, Task 3 |
| §5.1 双层代理 | Task 1 §2, Task 4 |
| §5.2 Camoufox | Task 1 §5 + 既有 smoke |
| §6 运营手册 | Task 1 §3–4 |
| §7 监控与成功标准 | Task 1 §6–7, Task 6 |
| §8 非目标 | Global Constraints（禁止多池代码） |
| §9 落点 | Task 3 `.env.dev`, Task 5 链接 |

## Placeholder / consistency self-review

- 无 TBD 任务；代理真实 URL 仅本地 `.env.dev` / 操作者填写。
- 阶段 0 数字与 spec 一致：`FREQUENCY=2`, `BATCH=80`, `WORKER=6–8`, `MIN=15`, `MAX=720`, `TIMEOUT=90`。
- 不引入新 Go 包或 rotator 行为变更。
- API curl 标注以实例为准，避免绑死错误路径时操作者可改用 UI。

## Risk notes

1. **只加 WORKER 不加代理** → 429 恶化；runbook 已强调。  
2. **误提交 `.env.dev`** → Task 3 强制 check-ignore。  
3. **同域不同 proxy_url** → CF 缓存失效、solve 打满；Task 4 强制同 URL。  
4. **不重启进程** → 旧 `POLLING_*` / `Default()` 缓存仍生效。

---

## Execution handoff

Plan 完成后由控制器提供两种执行方式（见 skill）。本计划以 **文档 + 本地配置** 为主，适合 **Inline Execution** 快速落地；若拆给多 agent，用 Subagent-Driven 亦可。
