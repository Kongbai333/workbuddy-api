package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSecurityHeadersOnHTML 面板 HTML 文档必须带全套安全头。
func TestSecurityHeadersOnHTML(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}), nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("HTML 文档缺少 CSP")
	}
	// 必须含的防御项 —— 这三条是本中间件真正兜得住的部分。
	for _, sub := range []string{"frame-ancestors 'none'", "object-src 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, sub) {
			t.Errorf("CSP 缺少 %q\ngot: %s", sub, csp)
		}
	}
}

// TestCSPNotOnAPI 挂 CSP 的前提是「响应确实是 HTML」。
// 给 JSON 端点挂 CSP 既无作用，又会污染 curl 调试时的响应头。
func TestCSPNotOnAPI(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}), nil)

	cases := []struct{ method, path string }{
		{"POST", "/v1/chat/completions"},
		{"GET", "/v1/models"},
		{"GET", "/api/state"},
		{"GET", "/status"},
		{"GET", "/healthz"},
		{"GET", "/app.js"},
		{"GET", "/style.css"},
		{"GET", "/logo64.png"},
		{"GET", "/v1/models"}, // 前缀判定优先于后缀
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if got := rec.Header().Get("Content-Security-Policy"); got != "" {
			t.Errorf("%s %s 不该带 CSP，实际 %q", c.method, c.path, got)
		}
		// 但 nosniff 这类与内容类型无关的头，任何响应都该有。
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s %s 缺少 nosniff", c.method, c.path)
		}
	}
}

// TestCSPOnPanelSubpath 面板若有子路径（非根）也应带 CSP。
func TestCSPOnPanelSubpath(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), nil)
	for _, p := range []string{"/", "/index.html", "/panel", "/v2/state"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("GET %s 应带 CSP", p)
		}
	}
}

// TestStatusRecorderCapture 访问日志依赖 statusRecorder 抓到的状态码与字节数。
func TestStatusRecorderCapture(t *testing.T) {
	var logLine string
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	}), func(f string, a ...any) {
		logLine = strings.TrimSpace(fmt.Sprintf(f, a...))
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/state", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("状态码 = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if logLine == "" {
		t.Fatal("未产生访问日志")
	}
	for _, sub := range []string{"status=418", "bytes=5", "/api/state"} {
		if !strings.Contains(logLine, sub) {
			t.Errorf("访问日志缺少 %q\ngot: %s", sub, logLine)
		}
	}
}

// TestStatusRecorderImplicit200 未显式 WriteHeader 时应记 200（Go 的隐式行为）。
func TestStatusRecorderImplicit200(t *testing.T) {
	rec := httptest.NewRecorder()
	s := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	_, _ = s.Write([]byte("xyz"))
	if s.status != http.StatusOK {
		t.Errorf("status = %d, want 200", s.status)
	}
	if s.bytes != 3 {
		t.Errorf("bytes = %d, want 3", s.bytes)
	}
}

// TestStatusRecorderFirstWriteHeaderWins 重复 WriteHeader 只记第一个。
func TestStatusRecorderFirstWriteHeaderWins(t *testing.T) {
	rec := httptest.NewRecorder()
	s := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	s.WriteHeader(http.StatusOK)
	s.WriteHeader(http.StatusInternalServerError)
	if s.status != http.StatusOK {
		t.Errorf("status = %d, want 200（首个 WriteHeader 应胜出）", s.status)
	}
}

// TestStatusRecorderFlushSSE SSE 流式响应依赖 Flusher 透传，
// 否则 Flush 不生效会把流式响应缓冲成一次性返回。
func TestStatusRecorderFlushSSE(t *testing.T) {
	fr := httptest.NewRecorder()
	s := &statusRecorder{ResponseWriter: fr, status: http.StatusOK}
	// httptest.ResponseRecorder 实现了 http.Flusher，透传后应被识别。
	_, ok := s.ResponseWriter.(http.Flusher)
	if !ok {
		t.Fatal("前置条件不成立：httptest.ResponseRecorder 应实现 http.Flusher")
	}
	s.Flush()
	if !fr.Flushed {
		t.Error("Flush 未透传到下游")
	}
}
