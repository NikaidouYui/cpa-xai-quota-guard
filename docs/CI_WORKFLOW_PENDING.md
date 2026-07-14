# CI workflow 待推送说明

本地已改好的商店兼容 CI 在：

- `.github/workflows/build.yml`（工作区修改，可能因 OAuth 缺 `workflow` scope 无法 push）
- 备份：`docs/ci-build.yml.store-compatible`

推送 workflow 需要：

```bash
gh auth refresh -s repo,workflow
# 或 classic PAT 勾选 repo + workflow
git add .github/workflows/build.yml
git commit -m "ci: package store-compatible release assets"
git push origin main
```

在此之前请用手动/已发布的 v0.3.10 规范资产，避免重新发布旧布局 zip。
