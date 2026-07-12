# cpa-xai-quota-guard

CLIProxyAPI **原生 Go 插件**（当前版本 **0.1.27**）：仅针对 **xAI** 登录凭证做额度/死号管控，并提供管理 UI 与用量统计。

## 做什么

1. 监听 `usage.handle`（成功计用量；失败按规则处理）
2. **仅** `provider=xai`
3. **HTTP 429 + `subscription:free-usage-exhausted`（滚动 24h）** → 临时禁用（`plugin_auto`），到期自动恢复
4. **403 permission-denied / 401 invalid credentials / 402 spending-limit** → **DELETE** 凭证（不可恢复）
5. 状态标签持久化：`plugin_auto` / `user_manual`
6. ticker 到期后**只恢复**本插件自动禁用的账号
7. 用户手动禁用永不自动启用
8. 管理页：状态栏（凭证数 / 总额度估 / 已用今日·总计 / 滚动池）、账号表分页、focus 视图

## 明确不做

- 不处理 Codex / OpenAI / Gemini / NVIDIA 等其它 provider
- 不处理网络错误、`context canceled`、HTTP 200 流式中断、5xx 等非业务额度错误
- **不照搬** Codex 的 `usage_limit_reached` / `x-codex-*` 窗口逻辑
- 时间解析失败时 **不禁用**（记日志，静默跳过）
- 「失败>0 且从未成功」账号默认 **不批量清理**（需人工决策）

## 错误处理矩阵

| 场景 | 条件（摘要） | 动作 |
|------|----------------|------|
| 免费额度用尽 | 429 + free-usage-exhausted / rolling 24h | `plugin_auto` 冷却，默认约 24h 内到点恢复 |
| 权限拒绝 | 403 + permission-denied | DELETE auth-files |
| 凭证失效 | 401 + invalid/expired / no auth context 等 | DELETE |
| 订阅/积分耗尽 | 402 + spending-limit / run out of credits | DELETE |
| 客户端取消 | 200 SSE + `context canceled` | **忽略** |
| 其它 4xx/5xx/网络 | — | **忽略** |

详情与字段白名单见 [DESIGN.md](./DESIGN.md)。

## 配置

`plugins.configs.cpa-xai-quota-guard` 示例（**勿提交真实 key**）：

```yaml
plugins:
  configs:
    cpa-xai-quota-guard:
      enabled: true
      tick_seconds: 30
      max_reset_seconds: 86400
      min_reset_seconds: 0
      management_url: "http://127.0.0.1:8317"
      management_key: "<CPA_MANAGEMENT_KEY>"
      state_path: "data/cpa-xai-quota-guard-state.json"
      include_unobserved_quota_est: true
      cpamp_url: "http://<CPAMP_HOST>:<PORT>"   # 可选，回补用量
      cpamp_admin_key: "<PLUS_ADMIN_KEY>"       # 可选
      webhook_url: ""                           # 可选
```

| 字段 | 默认 | 说明 |
|------|------|------|
| `enabled` | `false` | 总开关 |
| `tick_seconds` | `15` | 恢复扫描周期（改此值可热加载插件 so） |
| `max_reset_seconds` | `86400` | 重置等待上限 |
| `min_reset_seconds` | `0` | 最小冷却地板，0=不限制 |
| `management_url` | `""` | CPA 管理 API 基址 |
| `management_key` | `""` | `X-Management-Key`（敏感，UI 不回显） |
| `state_path` | `data/cpa-xai-quota-guard-state.json` | 持久化状态 |
| `include_unobserved_quota_est` | `true` | 总额度是否含未观测账号×默认 1M |
| `cpamp_url` / `cpamp_admin_key` | 空 | 可选 CPAMP 回补 |
| `webhook_url` | 空 | 可选事件回调 |

未配置 management 时只记日志，不操作账号。

## 管理 API / UI

| 路径 | 说明 |
|------|------|
| `GET /v0/management/cpa-xai-quota-guard/state?view=focus\|all` | 状态；默认 focus |
| `GET /v0/management/cpa-xai-quota-guard/config` | 非敏感配置摘要 |
| `POST /v0/management/cpa-xai-quota-guard/toggle` | 开关 |
| `POST /v0/management/cpa-xai-quota-guard/run` | 立即扫描恢复 |
| `POST /v0/management/cpa-xai-quota-guard/inject` | 注入测试事件（危险） |
| `GET /v0/resource/plugins/cpa-xai-quota-guard/index.html` | 管理 UI |

### focus 视图（性能）

全量 xAI inventory 可达数千。默认 **`view=focus`**：

- 只物化：跟踪中（冷却/手动）、CPA 已禁用、**今日有用量/请求** 的账号
- 今日「热」账号硬截断 **80** 条（`hot_hidden` 计入 summary）
- `auth-files` List：**TTL 缓存 + 失败 sticky 回落**，避免 UI 凭证数闪 0
- `state.inventory`：`{ok, stale, error, age_ms, xai_total}`

`view=all` 仅在筛选「全部账号 / 仅库存」时使用，体量更大、更慢。

### 状态栏指标（仅 xAI）

- **凭证数**：inventory 中 `provider=xai` 数量（与列表同源）
- **总额度(估)**：已知 rolling limit + 可选未观测×1M
- **已用今日 / 总计**：以 `usage.handle` 真实 token 为主；可选 CPAMP 回补地板
- **滚动池 used/limit**：来自 free-usage 报错中的 actual/limit 快照

## 构建

需 Go 1.26+ 与 CGO。示例（Linux amd64 / zig cc）：

```bash
export CGO_ENABLED=1
# 可选：replace 指向本地 CLIProxyAPI 源码
go test ./internal/xaiquota/ -count=1
go build -buildmode=c-shared -o bin/cpa-xai-quota-guard.so .
```

Windows：

```bash
# CGO_ENABLED=1 go build -buildmode=c-shared -o bin/cpa-xai-quota-guard.dll .
```

或使用 `.github/workflows/build.yml`（`workflow_dispatch`）。

## 测试

```bash
go test ./internal/xaiquota/ -count=1
```

覆盖：free-usage 匹配、401/402/403 删除判定、解析失败不禁用、手动禁用不恢复、到期只恢复 `plugin_auto`、用量/指标。

## 部署

1. 将 `cpa-xai-quota-guard.so` 放入 CPA `plugins/<os>/<arch>/`
2. 配置 `plugins.configs.cpa-xai-quota-guard`（见上）
3. `enabled: true`；改 `tick_seconds` 可触发插件热加载
4. 打开 UI 或调用 `state`，确认 `version` 与 `metrics.xai_total`

## 所有权模型

与 CPAMP 冷却一致：

- owner = `cpa_xai_quota_plugin`
- 禁用前若账号已禁用且无本插件 ownership → `user_manual`
- 到期恢复仅 `plugin_auto` 且非 `pre_disabled`

## 版本摘要

| 版本 | 要点 |
|------|------|
| 0.1.20–0.1.21 | 状态栏合并、总额度含未观测 |
| 0.1.22 | 401 invalid credentials → 删除 |
| 0.1.23 | 402 spending-limit → 删除 |
| 0.1.24–0.1.25 | state focus + UsageAndQuotaMaps 单次读 |
| 0.1.26 | focus 热账号 cap、loadState 防卡死 |
| **0.1.27** | auth-files sticky、inventory 元数据、UI 凭证数不闪 0 |

## 安全

- **禁止**提交真实 management key / CPAMP admin key / OAuth token / Cookie
- 配置只走环境变量或主机私有 `config.yaml`
- 日志对 body 截断；文档示例使用 `<CPA_MANAGEMENT_KEY>` 占位符
- 本地探针/补丁脚本（`_*.py` / `_*.sh`）默认不进仓库

## 设计文档

完整状态机、匹配字段、局限与演进：[DESIGN.md](./DESIGN.md)