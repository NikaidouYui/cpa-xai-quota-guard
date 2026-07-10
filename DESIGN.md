# cpa-xai-quota-guard 设计文档

> xAI 专用短时额度/限流管控插件（CLIProxyAPI native Go）

## 1. 目标

仅针对 **xAI** 登录凭证：当上游返回明确的「短时调用额度/限流耗尽」错误时，临时禁用该凭证；解析重置时间，到期后仅恢复本插件自动禁用的账号。用户手动禁用永不自动启用。

## 2. 硬性约束

1. 只监控 xAI（`provider`/`auth_type` = `xai`），其它 provider 全部忽略。
2. 仅捕获明确短时额度耗尽/限流类报错；网络/鉴权/封禁/月度额度/接口其它错误全部跳过。
3. 从报错 body/headers 解析冷却/重置时间，计算等待时长。
4. 合法报错 → 临时禁用对应登录凭证文件。
5. 重置时间到达 → 自动启用。
6. 仅恢复本插件自动禁用的文件。
7. 用户手动禁用永远不会被自动启用。
8. 状态标签：`plugin_auto` / `user_manual`，持久化。
9. 时间解析失败 → 不禁用，记日志，静默跳过。

## 3. 与 CPAMP 的对齐（模型，不是 Codex 规则）

复用 CPAMP `RateLimitAutoDisableWorker` 的所有权模型，**不复用 Codex 匹配字段**：

| 概念 | 取值 |
|------|------|
| owner | `cpa_xai_quota_plugin` |
| pre_disabled | 禁用前读到的 `disabled` |
| recover_at | 解析出的未来重置时间 |
| 恢复条件 | owner 匹配 + 非 pre_disabled + 到期 |

禁用前：

1. 读 management `auth-files` 当前 `disabled`。
2. 若已禁用且本插件无 active cooldown → 记为 `user_manual`，跳过。
3. 若本插件已有 cooldown → 可延长 `recover_at`。
4. 否则禁用并写 `plugin_auto`。

## 4. xAI 真实机制（不照搬 Codex）

### 4.1 与 Codex 的差异

| 维度 | Codex | xAI（本插件） |
|------|-------|----------------|
| provider | codex | **仅 xai** |
| 主信号 | `usage_limit_reached` / `x-codex-*` 窗口 | **HTTP 429 + rate_limit 语义** |
| 重置 | `resets_at` / Codex header windows | **Retry-After / x-ratelimit-reset* / body retry_after** |
| 月度/账户额度 | 部分走 usage_limit | **通常非本插件处理**（402/403/insufficient_quota 等跳过） |

### 4.2 合法匹配条件（全部满足）

1. `Failed == true`
2. Provider 规范化后为 `xai`（`auth_type=xai` 可作为兜底）
3. `StatusCode == 429`
4. body 或 headers 具备 **明确短时限流信号**（见下）
5. 成功解析出 **未来** 的 `recover_at`
6. `recover_at - now <= max_reset_seconds`（默认 86400）

任一不满足 → 跳过，不禁用。

### 4.3 短时限流信号（xAI / OpenAI-compatible）

xAI Responses API 走 OpenAI 兼容错误形态。以下任一成立即可（在 429 前提下）：

**Body：**

- `error.code` ∈ {`rate_limit_exceeded`, `rate_limit`, `too_many_requests`}
- `error.type` ∈ {`tokens`, `requests`, `rate_limit_error`, `rate_limit_exceeded`}
- message 明确含：`rate limit` / `too many requests` / `tokens per minute` / `requests per minute` / `TPM` / `RPM` / `rate_limit_exceeded`

**Headers（辅助确认 + 取重置时间）：**

- `Retry-After`（秒或 HTTP-date）
- `x-ratelimit-remaining-requests` / `x-ratelimit-remaining-tokens` 为 `0`
- `x-ratelimit-reset-requests` / `x-ratelimit-reset-tokens` / `x-ratelimit-reset` 存在

### 4.4 明确排除（永不处理）

- 状态码：0/401/402/403/5xx/网络类
- body 含：`unauthorized` / `invalid_api_key` / `invalid_api_key` / `permission` / `banned` / `suspended` / `insufficient_quota` / `billing` / `payment_required` / `monthly` credit 耗尽等
- 纯泛 429 且 **既无限流信号也无任何可解析重置时间** → 跳过
- Codex 专用字段 **不得** 作为 xAI 主信号：`usage_limit_reached`、`x-codex-*` 窗口

### 4.5 重置时间解析优先级

1. Header `Retry-After`（相对秒或 HTTP-date）
2. Header `x-ratelimit-reset-requests` / `x-ratelimit-reset-tokens` / `x-ratelimit-reset`  
   - 数值 < 1e12 视为 Unix 秒；>= 1e12 视为毫秒；若为“剩余秒”小整数（<= max_reset）也可作相对秒
3. Body 字段：`retry_after` / `retry_after_ms` / `reset_at` / `resets_at` / `resetsAt` / `resets_in_seconds`
4. 嵌套 `error.*` 同名字段

解析失败或非未来 → **不禁用**，日志 + 静默跳过。

## 5. 状态机

```
active
  └─ 合法 xAI 短时限流 → auto_disabled (disable_source=plugin_auto)
auto_disabled
  └─ now >= recover_at → 启用 → active（仅 plugin_auto）
user_manual_disabled
  └─ 永不自动启用
```

## 6. 持久化

默认 `data/cpa-xai-quota-guard-state.json`：

```json
{
  "version": 1,
  "updated_at_ms": 0,
  "accounts": {
    "<auth_index>": {
      "auth_index": "",
      "file_name": "",
      "provider": "xai",
      "account": "",
      "disable_source": "none|plugin_auto|user_manual",
      "state": "active|auto_disabled|user_manual_disabled",
      "recover_at_ms": 0,
      "disabled_at_ms": 0,
      "pre_disabled": false,
      "owner": "cpa_xai_quota_plugin",
      "reason": "",
      "last_event_hash": ""
    }
  }
}
```

## 7. 集成

- `usage.handle`：实时失败事件
- ticker：扫描 due cooldown 并启用（无需 Codex 式 probe；到期直接 enable）
- 禁用/启用：`PATCH /v0/management/auth-files/status`
- 可选 management 路由：state / logs / toggle / run

## 8. 配置

| 字段 | 默认 | 说明 |
|------|------|------|
| enabled | false | 总开关 |
| tick_seconds | 15 | 恢复扫描周期 |
| management_url | "" | CPA 管理基址 |
| management_key | "" | X-Management-Key |
| state_path | data/cpa-xai-quota-guard-state.json | 状态文件 |
| max_reset_seconds | 86400 | 超过则不禁用 |

## 9. 假设与局限

- 本地未抓到完整官方 429 样例全文；按 xAI OpenAI-compatible 错误 + 常见 rate-limit 头实现。
- 若未来 xAI 改变错误 schema，优先扩 `match.go` 白名单字段，不改所有权模型。
- management_url/key 未配置时只记日志，不操作账号。