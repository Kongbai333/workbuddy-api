#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""创建 GitHub Release 并上传本地构建好的 Windows exe。

为什么写脚本而不是命令行拼 curl：
release body 里有中文、反引号、换行，shell 里层层转义极易出错
（本次尝试 `python -c` 内嵌 `json.dumps` 直接炸了）。
交给 Python 处理转义，一次成功。

用法：
    python build/publish_release.py
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

REPO = "sddvcm/workbuddy-api"
TAG = "v0.1.1"
TOKEN_FILE = r"C:\Users\Administrator\Desktop\GitHub API.txt"
EXE = r"C:\Users\Administrator\workbuddy-api\dist\workbuddy-api.exe"

BODY = """## ⚠️ v0.1.1 安全修复

**v0.1.1 移除了「首次启动自动从其他目录导入账号凭据」的功能。**

旧版在真首次启动时会探测两个目录（开发者的旧项目路径，以及
`%USERPROFILE%\wbsync\build\bin`），若其中存在 `auths/*.json` 就把里面的
登录态凭据（access/refresh token）复制进新目录。该行为存在三个问题：

1. 公开源码里暴露了开发者的私人部署路径；
2. 扫描用户主目录属于**未经请求的数据获取**；
3. 静默复制凭据是明确的隐私问题，用户完全无从预期。

若你从 v0.1.0 升级且不需要该行为，**直接替换 exe 即可**。
需要迁移账号时，请在面板里重新登录，或手工把 `auths/*.json` 拷入程序目录。

## 快速开始

1. 下载 `workbuddy-api-windows-amd64.exe`，放到任意目录
2. 双击启动 —— **首次启动会自动生成一个随机 API Key 并打印在日志里**
3. 浏览器打开面板，添加账号

## 接入 AI 客户端

    Base URL: http://127.0.0.1:7863/v1
    API Key:  见面板顶栏（点击可复制）或 config.json 的 api_key

模型 ID 需带渠道前缀：`workbuddy/glm-5.3-flash`、`traework/<model>` 等。

## 主要特性

- **多渠道账号池** —— 统一调度、冷却、限流避让，按策略挑号（临期优先 / 积分优先 / 负载均衡）
- **会话粘性** —— 同一段对话恒定走同一账号，上游 prompt 缓存可命中，避免工具调用 ID 对不上
- **WAF IP 级 fail-fast** —— 多个账号接连撞 WAF 403 时判定为出口 IP 被拦，立即失败而非继续轮转
- **费率与模型清单** —— 11 个渠道的模型与倍率，支持搜索与比价
- **积分明细** —— 按账号查看可用 / 不可用积分小计
- **自动签到** —— 每日定时签到与保活
- **OpenAI / Anthropic 双协议兼容**

## 安全说明

首次启动会**随机生成** API Key（不使用固定默认值 —— 固定值会随二进制公开，
任何人下载 exe 就能反查到默认 key）。若监听 `0.0.0.0` 而 `api_key` 为空，
程序会拒绝启动，避免上游额度被局域网任意设备消耗。

面板监听非环回地址时**必须**配置 `admin_password`。

## 系统要求

- Windows 10+（本 Release 的手传资产仅 Windows amd64）
- 无需管理员权限

macOS / Linux 版本由 GitHub Actions 构建，见该 Release 的其他附件。

## 免责声明

本项目只是各上游渠道的集成与聚合，本身不涉及任何逆向分析和破解工作。
各渠道接口参数、签名算法等底层知识来自社区其他开源项目的公开成果。

仅供个人学习使用，请勿用于商业用途或违反上游服务条款的场景。
"""


def token() -> str:
    with open(TOKEN_FILE, encoding="utf-8") as f:
        return f.read().strip()


def api(method: str, url: str, data=None, headers=None, raw=False):
    req = urllib.request.Request(url, method=method, data=data)
    req.add_header("Authorization", "token " + token())
    req.add_header("Accept", "application/vnd.github+json")
    req.add_header("User-Agent", "workbuddy-api-publish")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            body = r.read()
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", "replace")
        raise SystemExit(f"HTTP {e.code}: {detail[:500]}")


def main():
    if not os.path.exists(EXE):
        raise SystemExit(f"找不到 {EXE}")

    # 上传前自查：二进制里绝不能有凭证
    data = open(EXE, "rb").read()
    for pat in (b"WildWorkAPI", b"ghp_", b"github_pat_"):
        if pat in data:
            raise SystemExit(f"!! 二进制含敏感内容 {pat!r}，中止上传")
    print(f"exe 自检通过（{len(data)/1024/1024:.1f} MB，无凭证）")

    # Release 可能已由 Actions 建好（tag 推送会触发），先查再决定
    rels = api("GET", f"https://api.github.com/repos/{REPO}/releases")
    rel = next((r for r in rels if r["tag_name"] == TAG), None)

    if rel is None:
        payload = json.dumps({
            "tag_name": TAG,
            "name": TAG,
            "body": BODY,
            "draft": False,
            "prerelease": False,
        }).encode("utf-8")
        rel = api("POST", f"https://api.github.com/repos/{REPO}/releases",
                  data=payload, headers={"Content-Type": "application/json"})
        print("Release 已创建:", rel["html_url"])
    else:
        print("Release 已存在，更新说明:", rel["html_url"])
        payload = json.dumps({"body": BODY, "name": TAG}).encode("utf-8")
        rel = api("PATCH", rel["url"], data=payload,
                  headers={"Content-Type": "application/json"})

    # 已有同名资产则先删（同名上传会 422）
    existing = {a["name"] for a in rel.get("assets", [])}
    name = "workbuddy-api-windows-amd64.exe"
    if name in existing:
        old = next(a for a in rel["assets"] if a["name"] == name)
        api("DELETE", old["url"])
        print(f"已删除同名旧资产 {name}")

    # 上传：走 assets_url，必须带 ?name=
    url = rel["upload_url"].split("{")[0] + f"?name={name}"
    boundary = "----workbuddyapi" + str(int(time.time()))
    with open(EXE, "rb") as f:
        blob = f.read()
    body_bytes = b"".join([
        f"--{boundary}\r\n".encode(),
        b'Content-Disposition: form-data; name="file"; filename="' +
        name.encode() + b'"\r\n',
        b"Content-Type: application/octet-stream\r\n\r\n",
        blob, b"\r\n", f"--{boundary}--\r\n".encode(),
    ])
    asset = api("POST", url, data=body_bytes, headers={
        "Content-Type": f"multipart/form-data; boundary={boundary}",
        "Content-Length": str(len(body_bytes)),
    })
    print(f"资产上传完成: {asset['name']}  {asset['size']/1024/1024:.1f} MB")
    print(f"  下载: {asset['browser_download_url']}")

    final = api("GET", f"https://api.github.com/repos/{REPO}/releases/tags/{TAG}")
    print("\n最终状态:")
    print(f"  {final['html_url']}")
    print(f"  资产 {len(final['assets'])} 个:")
    for a in final["assets"]:
        print(f"    {a['name']}  {a['size']/1024/1024:.1f} MB")


if __name__ == "__main__":
    main()