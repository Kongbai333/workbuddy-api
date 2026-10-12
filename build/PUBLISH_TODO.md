# 待执行：推送代码并发布 Release

## 1. 给 token 加 `workflow` scope（需你在网页操作）

1. 打开 https://github.com/settings/tokens
2. 点你那个 token（名字可能类似 "GitHub API"）→ **Regenerate / 编辑**
   - 注意：**Personal Access Token 无法追加 scope，只能重新生成**。
     重新生成时勾选：
     - `repo`（已有）
     - **`workflow`**（新勾，这是被拒的原因）
     - 如需自动发版，再勾 `Actions` 相关权限（`actions: write` 用于创建 release）
3. 生成后**覆盖**桌面那个 `GitHub API.txt` 里的内容（只放 token 一行，无换行无空格）

> 重新生成会让旧 token 立即失效 —— 这是好事，顺带完成了「用完即弃」。

## 2. 拿到新 token 后告诉我

我会执行：

```
git push -u origin main              # 推送 34 个提交
git tag v0.1.0 && git push origin v0.1.0
```

tag 推送会触发 `.github/workflows/build.yml`，由 GitHub Actions 构建
5 个平台（Windows/macOS×2/Linux×2）并自动创建 Release。

## 3. 同时用本地 exe 手动发布（不依赖 Actions）

Actions 构建需要几分钟，且 macOS 需要 CGO。如果你想立刻拿到 Windows 版本，
我直接走 API 上传本地已构建好的 exe（已验证含图标、已清除密钥）：

```
POST /repos/sddvcm/workbuddy-api/releases
  tag_name=v0.1.0, name=v0.1.0, prerelease=false
  body=<release 说明>
上传资产：workbuddy-api.exe (14.6 MB)
```

**两条路可以都做**：Actions 产出全套平台，手动补一个立刻可用的 Windows 版。
（同名资产 GitHub 会保留先传的那个，或我改成 `workbuddy-api-windows-amd64.exe`。）

## 已就绪的内容

| 项 | 状态 |
|---|---|
| 本地 34 个提交 | ✅ 已清理凭证，待推送 |
| 分支 `master` → `main` | ✅ 已重命名 |
| remote origin | ✅ 已配置（URL 不含 token） |
| exe 二进制 | ✅ 含图标、无任何凭证、15.2 MB |
| `.gitignore` | ✅ 忽略 `auths/` `config.json` `data/` `github_token.txt` |
| 全仓凭证扫描 | ✅ 真实 API Key 已清除并改为随机生成 |

## Release 说明草稿

标题：`v0.1.0`

```markdown
workbuddy-api v0.1.0

多渠道账号聚合工具 —— 把 WorkBuddy / TraeWork / Qoder / TraeCode /
QoderCN / QoderCom / QwenWork / MonkeyCode / Raccoon / Loomy / Oczen
等渠道的账号统一成一个 OpenAI / Anthropic 兼容接口。

## 快速开始

1. 下载 `workbuddy-api-windows-amd64.exe`，放到任意目录
2. 双击启动（首次会生成随机 API Key 并写入 config.json）
3. 浏览器打开面板，添加账号

## 接入客户端

    Base URL: http://127.0.0.1:7863/v1
    API Key:  见面板顶栏（或 config.json 的 api_key）

模型 ID 需带渠道前缀：`workbuddy/glm-5.3-flash`、`traework/<model>` …

## 主要特性

- **多渠道账号池**：统一调度、冷却、限流避让，按策略挑号
- **会话粘性**：同一段对话恒定走同一账号，上游 prompt 缓存可命中
- **WAF IP 级 fail-fast**：多个账号接连撞 WAF 403 时判定为出口 IP 被拦，
  立即失败而非继续轮转（避免加重风控）
- **费率与模型清单**：11 个渠道的模型与倍率，支持搜索与比价
- **积分明细**：按账号查看可用/不可用积分小计
- **自动签到**：每日定时签到与保活
- **跨平台**：Windows / macOS / Linux

## 系统要求

- Windows 10+ / macOS 11+ / Linux（glibc 2.28+）
- 无需管理员权限（托盘模式除外）

## 安全提示

面板监听 `0.0.0.0`（或任何非 `127.0.0.1` 地址）时**必须配置管理密码**。
首次启动会随机生成 API Key，请勿删除 —— 删掉后任何拿到二进制的人都能猜到固定默认值。

## 免责声明

本项目只是各上游渠道的集成与聚合，本身不涉及任何逆向分析和破解工作。
各渠道接口参数、签名算法等底层知识来自社区其他开源项目的公开成果。
仅供个人学习使用，请勿用于商业或违反上游服务条款的用途。
```

## 已知限制

- macOS 版本需 CGO，Actions 构建可能较慢
- Linux 版未做静态链接，依赖 glibc 2.28+
- 部分渠道为单账号池，账号耗尽即该渠道不可用（面板会提示）