# CI workflow 说明

商店兼容 CI 已写入：

- `.github/workflows/build.yml`（主 workflow）
- 备份：`docs/ci-build.yml.store-compatible`

推送 workflow 需要 GitHub token 具备 **`workflow` scope**：

```bash
gh auth refresh -s repo,workflow
# 或 classic PAT 勾选 repo + workflow
git push origin main
```

Release 资产约定（`v*` tag 触发）：

- zip：`cpa-xai-quota-guard_{version}_{goos}_{goarch}.zip`
- zip 根目录：`cpa-xai-quota-guard.so|.dll|.dylib`
- 同 Release：`checksums.txt`
