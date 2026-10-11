//go:build !windows

package auth

import (
	"strings"
	"testing"
)

// TestDecryptSecretRejectsDPAPIOnOtherPlatform 锁定非 Windows 平台的行为：
// dpapi: 密文必须解成**空串**，而不是原样返回密文 ——
// 原样返回会让上层拿密文当 token 发给上游，换回一堆难以理解的 401。
func TestDecryptSecretRejectsDPAPIOnOtherPlatform(t *testing.T) {
	if got := DecryptSecret("dpapi:AQAAANCMfakeciphertext"); got != "" {
		t.Errorf("dpapi: 密文应解为空串，got %q", got)
	}
	if got := DecryptSecret("plain-token"); got != "plain-token" {
		t.Errorf("明文应原样返回，got %q", got)
	}
	if got := EncryptSecret("plain-token"); got != "plain-token" {
		t.Errorf("非 Windows 应明文直通（不做自造加密），got %q", got)
	}
}

// TestParseFailsOnDPAPIFromWindows 跨平台迁移陷阱：
// 把 Windows 上生成的 auths 目录直接拷到 Linux / 容器时，Parse 必须**报错**
// （凭证不可用），而不是返回一个 token 为空串的 Auth ——
// 后者会让账号进入池子、每次请求都失败，比直接拒绝加载更难排查。
func TestParseFailsOnDPAPIFromWindows(t *testing.T) {
	body := []byte(`{"auth":{"accessToken":"dpapi:AQAAANCMfake",` +
		`"refreshToken":"dpapi:AQAAANCMfake","expiresAt":1893456000,` +
		`"domain":"www.codebuddy.cn"},"account":{"uid":"u1"}}`)
	if _, err := Parse(body); err == nil {
		t.Fatal("含 Windows DPAPI 密文的凭据在当前平台必须解析失败")
	} else if !strings.Contains(err.Error(), "accessToken") {
		t.Errorf("错误信息应指明 accessToken 缺失，got %v", err)
	}
}
