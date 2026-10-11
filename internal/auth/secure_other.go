//go:build !windows

// Package auth 在非 Windows 平台（Linux / macOS，含 Docker 容器）的凭证保护实现。
//
// ★ 关键设计说明（部署前必读）
//
// Windows 上用 DPAPI 加密 token：密文与「当前 Windows 用户」绑定，
// 换用户 / 换机器都解不开。Linux 上没有系统级的等价物，因此这里的策略是
// **不加密，改为依赖文件权限**：
//   - EncryptSecret 直接返回明文（不写 dpapi: 前缀）
//   - DecryptSecret 对带 dpapi: 前缀的值返回空串（见下方「跨平台迁移陷阱」）
//
// 安全边界（部署者必须知情）：
//
//	凭证以明文存放在 auths/*.json，安全性完全取决于：
//	  1. 宿主机的文件系统权限（推荐 chmod 600 + 非 root 运行）
//	  2. 容器隔离边界（不要把数据目录挂到共享位置）
//	因此**不要把 NAS 上的数据目录暴露给其他容器或用户**。
//
// 跨平台迁移陷阱（重要）：
//
//	在 Windows 上生成的 auths/*.json 里 token 是 dpapi: 密文，
//	**在 Linux 上无法解密**（DPAPI 密钥在 Windows 用户配置里）。
//	所以从 Windows 迁移到 NAS/容器时，必须在目标平台**重新登录一次**，
//	不能只拷贝 auths/ 目录。DecryptSecret 遇到 dpapi: 前缀会返回空串并打日志，
//	让上层明确感知「凭证不可用」，而不是拿密文当 token 去请求上游
//	（那样只会得到一堆难以理解的 401）。
package auth

import (
	"log"
	"strings"
)

// dpapiPrefix 跨平台保留：用于识别「来自 Windows 的加密值」。
const dpapiPrefix = "dpapi:"

// EncryptSecret 非 Windows 平台直接返回明文。
//
// 不做任何「自造加密」：在没有密钥管理的前提下，自造加密（例如固定密钥 AES）
// 只会制造安全假象，实际强度不如直接依赖文件权限。
func EncryptSecret(plain string) string {
	return plain
}

// DecryptSecret 非 Windows 平台处理入参。
//
//   - 明文（无 dpapi: 前缀）→ 原样返回
//   - dpapi: 密文（来自 Windows 的 auths 文件）→ 返回空串并告警
//
// 为什么返回空串而不是原样返回密文：若把密文当 token 发给上游，
// 会得到一串难以理解的 401，排查成本很高；返回空串能让上层明确判定
// 「该账号凭证不可用」，日志也更直观。
func DecryptSecret(s string) string {
	if strings.HasPrefix(s, dpapiPrefix) {
		log.Printf("auth: 检测到 Windows DPAPI 密文，当前平台无法解密 —— " +
			"该账号凭证不可用，请在当前平台的管理面板里重新登录")
		return ""
	}
	return s
}
