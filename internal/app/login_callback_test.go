package app

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

// panelLoginCallback 读一次 /api/state 里的登录回调相关字段。
func panelLoginCallback(t *testing.T, mux *http.ServeMux) State {
	t.Helper()
	w := doReq(mux, "GET", "/api/state", "", nil)
	if w.Code != 200 {
		t.Fatalf("/api/state 应 200，实际 %d", w.Code)
	}
	var st State
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("解析 /api/state：%v", err)
	}
	return st
}

// TestLoginCallbackEditableFromPanel 登录回调地址必须能在面板里改，而不是只能写死
// 在配置文件/环境变量里 —— 宿主机 IP 会变（换网络、DHCP 续租），写死一份迟早失效，
// 现象是 TraeWork / 千问办公 授权完成后，面板一直停在「等待完成」。
func TestLoginCallbackEditableFromPanel(t *testing.T) {
	a := newPanelApp(t, "127.0.0.1", "")
	mux := panelMux(a)

	// 初始：未配置（空串）+ 一条建议值供输入框占位
	st := panelLoginCallback(t, mux)
	if st.LoginCallback != "" {
		t.Fatalf("初始 login_callback 应为空串，实际 %q", st.LoginCallback)
	}
	if st.LoginCallbackSuggested == "" {
		t.Fatal("login_callback_suggested 不应为空（输入框占位要用）")
	}
	if !strings.HasPrefix(st.LoginCallbackSuggested, "http://") {
		t.Errorf("建议值应是完整 http 地址，实际 %q", st.LoginCallbackSuggested)
	}

	// 保存：允许省 scheme、允许带尾斜杠，落盘时被规范化
	if w := doReq(mux, "POST", "/api/config/login_callback",
		`{"login_callback":"192.168.1.10:17863/"}`, nil); w.Code != 200 {
		t.Fatalf("保存合法地址应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	if st = panelLoginCallback(t, mux); st.LoginCallback != "http://192.168.1.10:17863" {
		t.Fatalf("规范化后的值应为 http://192.168.1.10:17863，实际 %q", st.LoginCallback)
	}

	// 热生效：登录编排每次开始都重读 a.cfg.LoginCallback，不需要重启
	a.mu.Lock()
	got := a.cfg.LoginCallback
	a.mu.Unlock()
	if got != "http://192.168.1.10:17863" {
		t.Fatalf("内存配置应同步更新（否则要重启才生效），实际 %q", got)
	}

	// 已落盘：容器重建后 entrypoint 也要能读到面板里改过的值
	raw, err := os.ReadFile(a.cfgPath)
	if err != nil {
		t.Fatalf("读配置：%v", err)
	}
	if !strings.Contains(string(raw), `"login_callback": "http://192.168.1.10:17863"`) {
		t.Errorf("配置未落盘：%s", raw)
	}

	// 非法值一律拒绝，且不得把已存的值改坏
	for _, bad := range []string{
		`{"login_callback":"https://192.168.1.10:17863"}`,   // 回调监听是明文 HTTP
		`{"login_callback":"http://192.168.1.10:17863/cb"}`, // 路径由各渠道自己拼
		`{"login_callback":"http://192.168.1.10:99999"}`,    // 端口越界
		`不是 JSON`, // 解析失败不能当清空
	} {
		if w := doReq(mux, "POST", "/api/config/login_callback", bad, nil); w.Code != 400 {
			t.Errorf("%s 应 400，实际 %d", bad, w.Code)
		}
		if st = panelLoginCallback(t, mux); st.LoginCallback != "http://192.168.1.10:17863" {
			t.Fatalf("%s 被拒后原值不应变化，实际 %q", bad, st.LoginCallback)
		}
	}

	// 清空 = 回到「127.0.0.1 + 随机端口」的裸机行为（只输空白视同清空）
	for _, empty := range []string{`{"login_callback":""}`, `{"login_callback":"   "}`} {
		if w := doReq(mux, "POST", "/api/config/login_callback", empty, nil); w.Code != 200 {
			t.Fatalf("清空应 200，实际 %d", w.Code)
		}
		if st = panelLoginCallback(t, mux); st.LoginCallback != "" {
			t.Fatalf("清空后应为空串，实际 %q", st.LoginCallback)
		}
	}
}
