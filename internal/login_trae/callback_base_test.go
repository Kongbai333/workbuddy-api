package login_trae

// callback_base_test.go 回归测试：容器里的回调地址。
//
// 背景（真实故障）：容器内 127.0.0.1 指的是**容器自己**，且回调监听用的是
// 随机空闲端口 —— 宿主机/局域网的浏览器既访问不到那个地址，也没法用 -p 把
// 随机端口映射出去。表现是「Trae 授权页显示登录完成，面板却一直停在等待」，
// 日志里没有任何错误。
//
// 修复：config.LoginCallback 给出对外可达基址后，回调端口固定、监听 0.0.0.0，
// 广告出去的地址就是配置里那个。本用例锁定四件事：
//  1. 不配置时行为不变（127.0.0.1 + 随机端口）—— 裸机部署零回归；
//  2. 配置后回调地址逐字等于配置值 + /authorize，且该端口**真的在监听**；
//  3. 监听落在全部网卡（容器里绑宿主机 IP 必然失败）；
//  4. 配置有问题（缺端口 / 端口被占 / 带尾斜杠）时给出可行动的结果。

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freePort 取一个当前空闲的端口号（监听后立刻释放）。
// 仅用于测试构造回调基址；正式路径的端口由用户配置决定。
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

// callbackOf 取出授权 URL 里的 auth_callback_url 参数。
func callbackOf(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("解析授权 URL: %v", err)
	}
	raw := u.Query().Get("auth_callback_url")
	if raw == "" {
		t.Fatal("授权 URL 缺少 auth_callback_url")
	}
	return raw
}

// waitState 轮询等待回调写入 state（回调 handler 在响应前落盘，这里只做兜底）。
func waitState(t *testing.T, path string) state {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var st state
		if raw, err := os.ReadFile(path); err == nil && json.Unmarshal(raw, &st) == nil {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("state 文件 %s 未在超时前写入", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCallbackBaseDefaultIsLoopbackRandomPort 未配置时保持历史行为：
// 回调指向 127.0.0.1 的随机端口（裸机浏览器与程序同机，天然可达）。
func TestCallbackBaseDefaultIsLoopbackRandomPort(t *testing.T) {
	authURL, err := Start(NewClient(), filepath.Join(t.TempDir(), "login-state.json"), "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cb, err := url.Parse(callbackOf(t, authURL))
	if err != nil {
		t.Fatal(err)
	}
	if cb.Hostname() != "127.0.0.1" {
		t.Fatalf("默认回调主机应为 127.0.0.1，got %q", cb.Hostname())
	}
	if cb.Port() == "" || cb.Path != "/authorize" {
		t.Fatalf("默认回调应为 127.0.0.1:<随机端口>/authorize，got %q", cb.String())
	}
}

// TestCallbackBaseIsAdvertisedVerbatimAndReachable 配置基址后：
// 广告出去的地址就是配置值（容器外的浏览器按它访问），且端口确实在监听。
func TestCallbackBaseIsAdvertisedVerbatimAndReachable(t *testing.T) {
	base := "http://127.0.0.1:" + freePort(t)
	statePath := filepath.Join(t.TempDir(), "login-state.json")

	authURL, err := Start(NewClient(), statePath, base)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	want := base + "/authorize"
	if got := callbackOf(t, authURL); got != want {
		t.Fatalf("回调地址应为配置值本身：got %q want %q", got, want)
	}

	// 端口真的在监听：模拟浏览器带凭证回调，state 应被写入。
	// 走 127.0.0.1 而不是 0.0.0.0，顺带证明「容器把端口映射到宿主回环」这条路径可用。
	resp, err := http.Get(want + "?refreshToken=rt-cb-test")
	if err != nil {
		t.Fatalf("回调端口不可达（地址广告对了但没监听）：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("回调 HTTP %d，body=%s", resp.StatusCode, body)
	}
	if got := waitState(t, statePath); got.RefreshToken != "rt-cb-test" {
		t.Fatalf("回调未把凭证写进 state：got %q", got.RefreshToken)
	}
}

// TestCallbackBaseWithoutPortIsRejected 基址缺端口必须报错：
// 容器端口要能被 -p 映射，缺端口时无从映射，静默退回随机端口只会重演原故障。
func TestCallbackBaseWithoutPortIsRejected(t *testing.T) {
	_, err := Start(NewClient(), filepath.Join(t.TempDir(), "login-state.json"), "http://127.0.0.1")
	if err == nil {
		t.Fatal("缺端口的回调基址应报错")
	}
	if !strings.Contains(err.Error(), "端口") {
		t.Fatalf("错误文案应提示端口问题，got %v", err)
	}
}

// TestListenCallbackBindsAllInterfaces 锁定监听落在全部网卡：
// 容器里宿主机 LAN IP 不属于容器，绑具体 IP 必然 bind 失败。
func TestListenCallbackBindsAllInterfaces(t *testing.T) {
	base := "http://192.168.1.10:" + freePort(t)
	ln, gotBase, err := listenCallback(base)
	if err != nil {
		t.Fatalf("listenCallback: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if gotBase != base {
		t.Fatalf("对外基址应原样保留配置值，got %q", gotBase)
	}
	host, _, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsUnspecified() {
		t.Fatalf("监听应落在全部网卡（0.0.0.0/::），got %q", ln.Addr().String())
	}
}

// TestListenCallbackPortConflictIsReported 端口被占用时要给出可行动的错误，
// 而不是让用户对着「等待完成」猜。
func TestListenCallbackPortConflictIsReported(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	_, _, err = listenCallback("http://127.0.0.1:" + port)
	if err == nil {
		t.Fatal("端口被占用时应报错")
	}
	if !strings.Contains(err.Error(), "监听失败") {
		t.Fatalf("错误文案应指出监听失败，got %v", err)
	}
}

// TestCallbackBaseTrailingSlashTolerated 配置带尾斜杠（http://host:port/）时
// 不能拼出 //authorize —— 上游对回调地址是逐字符比对的。
func TestCallbackBaseTrailingSlashTolerated(t *testing.T) {
	authURL, err := Start(NewClient(), filepath.Join(t.TempDir(), "login-state.json"),
		"http://127.0.0.1:"+freePort(t)+"/")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := callbackOf(t, authURL); strings.Contains(got, "//authorize") {
		t.Fatalf("尾斜杠未清理，回调地址出现双斜杠：%q", got)
	}
}
