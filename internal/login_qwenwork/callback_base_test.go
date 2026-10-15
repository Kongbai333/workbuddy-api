package login_qwenwork

// callback_base_test.go 回归测试：容器里的 redirect_uri。
//
// 与 login_trae 同因（容器内 127.0.0.1 是容器自己、随机端口无法 -p 映射）：
// 不配置回调基址时，授权页会跳到一个容器外的浏览器永远打不开的地址，
// 表现为「授权完成、面板一直等待」。
//
// 这里额外锁定一条本渠道特有的约束：**redirect_uri 必须与兑换 token 时提交的
// 一字不差**（Hydra 逐字符比对），所以它必须落进 state 供 Poll 复用。

import (
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return strconv.Itoa(port)
}

// redirectURIOf 取出授权 URL 里的 redirect_uri 参数。
func redirectURIOf(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("解析授权 URL: %v", err)
	}
	raw := u.Query().Get("redirect_uri")
	if raw == "" {
		t.Fatal("授权 URL 缺少 redirect_uri")
	}
	return raw
}

// TestRedirectURIDefaultIsLoopbackRandomPort 未配置时保持历史行为。
func TestRedirectURIDefaultIsLoopbackRandomPort(t *testing.T) {
	defer Shutdown()
	authURL, err := Start(NewClient(), filepath.Join(t.TempDir(), "login-state.json"), "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ru, err := url.Parse(redirectURIOf(t, authURL))
	if err != nil {
		t.Fatal(err)
	}
	if ru.Hostname() != "127.0.0.1" || ru.Port() == "" || ru.Path != "/callback" {
		t.Fatalf("默认 redirect_uri 应为 http://127.0.0.1:<随机端口>/callback，got %q", ru.String())
	}
}

// TestRedirectURIUsesConfiguredBaseAndLandsInState 配置基址后：
// 授权 URL 与落盘的 state.RedirectURI 必须是同一个值（Poll 兑换时要用）。
func TestRedirectURIUsesConfiguredBaseAndLandsInState(t *testing.T) {
	defer Shutdown()
	base := "http://127.0.0.1:" + freePort(t)
	statePath := filepath.Join(t.TempDir(), "login-state.json")

	authURL, err := Start(NewClient(), statePath, base)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	want := base + "/callback"
	if got := redirectURIOf(t, authURL); got != want {
		t.Fatalf("redirect_uri 应为配置值本身：got %q want %q", got, want)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var st state
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	if st.RedirectURI != want {
		t.Fatalf("state.RedirectURI 与授权 URL 不一致（兑换 token 会被 Hydra 拒绝）：got %q want %q",
			st.RedirectURI, want)
	}
	if st.CallbackPort == 0 {
		t.Fatal("state.CallbackPort 未记录")
	}
}

// TestCallbackBaseWithoutPortIsRejected 缺端口必须报错（容器端口要能被 -p 映射）。
func TestCallbackBaseWithoutPortIsRejected(t *testing.T) {
	defer Shutdown()
	_, err := Start(NewClient(), filepath.Join(t.TempDir(), "login-state.json"), "http://127.0.0.1")
	if err == nil {
		t.Fatal("缺端口的回调基址应报错")
	}
	if !strings.Contains(err.Error(), "端口") {
		t.Fatalf("错误文案应提示端口问题，got %v", err)
	}
}
