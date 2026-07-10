# cpa-xai-quota-guard

CLIProxyAPI **原生 Go 插件**：仅针对 **xAI** 账号做「短时调用额度/限流耗尽 → 临时禁用 → 到点自动恢复」。

## 做什么

1. 监听 `usage.handle` 失败事件  
2. **仅** `provider=xai` 且 **HTTP 429 + 明确 rate-limit 信号 + 可解析重置时间** 时禁用  
3. 状态标签持久化：`plugin_auto` / `user_manual`  
4. ticker 到期后**只恢复**本插件自动禁用的账号  
5. 用户手动禁用永不自动启用  

## 明确不做

- 不处理 Codex / OpenAI / Gemini 等其它 provider  
- 不处理 401/403/402/5xx/网络错误  
- 不处理 `insufficient_quota` / 月度额度 / 封禁  
- **不照搬** Codex 的 `usage_limit_reached` / `x-codex-*` 窗口逻辑  
- 时间解析失败时**不禁用**

## xAI 匹配规则（摘要）

见 [DESIGN.md](./DESIGN.md)。核心：

| 条件 | 要求 |
|------|------|
| Provider | `xai` |
| Status | `429` |
| 信号 | `rate_limit_exceeded` / `type=tokens\|requests` / 限流文案 / ratelimit remaining=0 |
| 重置时间 | `Retry-After` / `x-ratelimit-reset*` / body `retry_after` 等，必须在未来 |

## 配置

| 字段 | 默认 | 说明 |
|------|------|------|
| `enabled` | `false` | 总开关 |
| `tick_seconds` | `15` | 恢复扫描周期 |
| `max_reset_seconds` | `86400` | 重置等待上限 |
| `management_url` | `""` | CPA 管理 API 基址，如 `http://127.0.0.1:8317` |
| `management_key` | `""` | `X-Management-Key` |
| `state_path` | `data/cpa-xai-quota-guard-state.json` | 持久化状态 |

未配置 `management_url`/`management_key` 时只记日志，不操作账号。

## 构建

本机需 Go 1.26+ 与 CGO。CI 参考：

```bash
git clone --depth 1 https://github.com/router-for-me/CLIProxyAPI ./CLIProxyAPI-src
printf '\nreplace github.com/router-for-me/CLIProxyAPI/v7 => ./CLIProxyAPI-src\n' >> go.mod
go mod tidy
CGO_ENABLED=1 go build -buildmode=c-shared -o bin/cpa-xai-quota-guard.so .
# Windows:
# CGO_ENABLED=1 go build -buildmode=c-shared -o bin/cpa-xai-quota-guard.dll .
```

或使用 `.github/workflows/build.yml`（`workflow_dispatch`）。

## 测试

纯逻辑包无需 CGO：

```bash
go test ./internal/xaiquota/ -count=1
```

覆盖：匹配成功/失败、解析失败不禁用、手动禁用不恢复、到期只恢复 `plugin_auto`。

## 部署

1. 将 `cpa-xai-quota-guard.so` / `.dll` 放入 CPA 插件目录  
2. 在管理界面配置 `management_url` / `management_key`  
3. 打开 `enabled`  
4. 观察状态文件或 management 路由：  
   - `GET /cpa-xai-quota-guard/state`  
   - `POST /cpa-xai-quota-guard/run`  

## 所有权模型

与 CPAMP 冷却一致：

- owner = `cpa_xai_quota_plugin`  
- 禁用前若账号已禁用且无本插件 ownership → `user_manual`  
- 到期恢复仅 `plugin_auto` 且非 `pre_disabled`

## 安全

- 禁止提交真实 management key / token  
- 日志对 body 做截断  