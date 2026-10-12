// Package httpauth Bearer 鉴权原语。
//
// ## 为什么单独成包
//
// 面板口令（internal/app/session.go）与 API Key（internal/server/handler.go）
// 是两处独立实现的鉴权。此前它们口径不一致：面板口令用常量时间比较，
// API Key 用 `!=` 直接比较 —— 同一个项目里两种口径，且后者带计时侧信道。
// 统一到这里后：口径只有一份，且天然常量时间。
package httpauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearerPrefix 认证方案前缀（大小写敏感，与 HTTP 规范一致）。
const bearerPrefix = "Bearer "

// VerifyBearer 校验请求头是否携带正确的 Bearer 密钥。
//
// key 为空表示「未启用鉴权」，恒返回 true（调用方据此放行）。
//
// ## 为什么「先摘要再比较」
//
// 直接对明文做 ConstantTimeCompare 仍有缺陷：长度不同的字符串会在
// `len` 不等时提前返回，**泄露密钥长度**；Go 的字符串比较还可能短路。
// 先各自算 SHA-256（定长 32 字节）再比，长度差异被完全吸收，
// 且摘要不可逆 —— 即使真存在侧信道也拿不到密钥原文。
func VerifyBearer(r *http.Request, key string) bool {
	if key == "" {
		return true
	}
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, bearerPrefix) {
		// 缺头/方案不对：仍走一次摘要比较，保持耗时形状一致
		// （否则「没有 Authorization 头」会明显快于「头但密钥错」，本身也是侧信道）。
		subtle.ConstantTimeCompare(digest(""), digest(key))
		return false
	}
	tok := authz[len(bearerPrefix):]
	return Equal(TrimSpace(tok), key)
}

// VerifyBearerOrAPIKeyHeader 同时接受 Anthropic 风格的 `x-api-key` 与标准
// `Authorization: Bearer`（Claude Code 用前者，OpenAI 客户端用后者）。
//
// 两个头都会参与比较：即使已通过其中一个，另一个也照比一次，
// 避免「只带 x-api-key 的请求明显更快」这种形状差异。
func VerifyBearerOrAPIKeyHeader(r *http.Request, key string) bool {
	if key == "" {
		return true
	}
	ok := VerifyBearer(r, key)
	xKey := TrimSpace(r.Header.Get("x-api-key"))
	if Equal(xKey, key) {
		ok = true
	}
	return ok
}

// Equal 常量时间字符串相等（先摘要，吸收长度差异）。
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare(digest(a), digest(b)) == 1
}

// TrimSpace 去空白（与 strings.TrimSpace 同义，单独提供以便本包自洽）。
func TrimSpace(s string) string { return strings.TrimSpace(s) }

// digest 返回 s 的 SHA-256（定长 32 字节，供常量时间比较）。
func digest(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}
