# 界面截图

本目录存放 README 与文档用的管理页截图。

## 当前文件

| 文件 | 说明 |
|------|------|
| `dashboard.png` | 状态栏 / 额度概览 |
| `patrol.png` | 主动巡查卡 |
| `accounts.png` | 账号状态表 |
| `*.svg` | 旧矢量占位（回退） |

## 如何重新生成（推荐）

使用真实 `web/console.html` + **合成脱敏 mock 数据** 渲染，不连线上：

```bash
python scripts/render_docs_screenshots.py
```

依赖：本机 Chrome + `pip install playwright`（脚本走系统 Chrome，无需 `playwright install`）。

生成物：

- `docs/screenshots/dashboard.png`
- `docs/screenshots/patrol.png`
- `docs/screenshots/accounts.png`

本地中间文件 `_render_demo.html` 已 gitignore，勿提交。

## 脱敏约定

Mock 数据内已使用：

- 邮箱：`user***@example.com` 形态
- 代理：`socks5://***:***@proxy.example:1080`
- 无 management key / token / cookie
- 数字为示意量级，非生产机密

切勿把真实管理页未脱敏截图提交进仓库。
