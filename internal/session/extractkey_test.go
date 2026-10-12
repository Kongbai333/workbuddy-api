package session

// extractkey_test.go 会话键提取的回归测试。
//
// 这一层的每个分支都对应一个**线上踩过的坑**，不能凭直觉简化：
//   - camelCase 不认 → 粘性永不命中（同一对话轮转不同账号 + 上游缓存 miss）
//   - user_id 当键 → 粒度过粗，一个用户的所有并行对话被钉到同一账号
//   - 派生键用全量消息哈希 → 每轮追加历史，键每轮变 → 粘性完全失效
//   - 派生键把图片 URL 入哈希 → 同图逐轮 URL 变 → 同样失效

import "testing"

func TestExtractKeyExplicitConversationIDs(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"metadata snake", `{"metadata":{"conversation_id":"c1"}}`, "c1"},
		{"metadata camel（真实客户端发的就是它）", `{"metadata":{"conversationId":"c2"}}`, "c2"},
		{"顶层 snake", `{"conversation_id":"c3"}`, "c3"},
		{"顶层 camel", `{"conversationId":"c4"}`, "c4"},
		{"prompt_cache_key", `{"prompt_cache_key":"c5"}`, "c5"},
		// 优先级：显式 conversation 维度必须压过 prompt_cache_key
		{"conversation 优先于 cache_key", `{"conversation_id":"c6","prompt_cache_key":"x"}`, "c6"},
		{"metadata 优先于顶层", `{"metadata":{"conversation_id":"c7"},"conversation_id":"c8"}`, "c7"},
		// 空串不算命中，应继续往后找
		{"空值跳过", `{"conversation_id":"","prompt_cache_key":"c9"}`, "c9"},
	}
	for _, c := range cases {
		if got := ExtractKey([]byte(c.body)); got != c.want {
			t.Errorf("%s: ExtractKey=%q want %q", c.name, got, c.want)
		}
	}
}

func TestExtractKeyUserIDSuppressesDerivation(t *testing.T) {
	// user 维度**既不作键、也抑制派生回退** —— 否则只发 user_id 的请求会
	// 借内容哈希重新获得粘性，使「不按 user 粘性」的契约在 fallback 路径失效。
	cases := []string{
		`{"user_id":"u1","messages":[{"role":"user","content":"hi"}]}`,
		`{"metadata":{"user_id":"u1"},"messages":[{"role":"user","content":"hi"}]}`,
	}
	for _, b := range cases {
		if got := ExtractKey([]byte(b)); got != "" {
			t.Errorf("带 user_id 时不应派生键：%s → %q", b, got)
		}
	}
}

func TestDeriveKeyStableAcrossTurns(t *testing.T) {
	// 多轮：每轮追加历史消息。派生键必须**保持不变**，否则粘性每轮失效。
	turn1 := `{"messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"第一问"}]}`
	turn2 := `{"messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"第一问"},{"role":"assistant","content":"答"},{"role":"user","content":"第二问"}]}`
	k1 := ExtractKey([]byte(turn1))
	k2 := ExtractKey([]byte(turn2))
	if k1 == "" || k2 == "" {
		t.Fatalf("应能派生键：k1=%q k2=%q", k1, k2)
	}
	if k1 != k2 {
		t.Fatalf("多轮派生键漂移（粘性失效）：%q → %q", k1, k2)
	}
}

func TestDeriveKeyDiffersAcrossConversations(t *testing.T) {
	a := ExtractKey([]byte(`{"messages":[{"role":"user","content":"问题A"}]}`))
	b := ExtractKey([]byte(`{"messages":[{"role":"user","content":"问题B"}]}`))
	if a == "" || b == "" {
		t.Fatal("都应能派生")
	}
	if a == b {
		t.Fatal("不同对话派生出了相同键（会互相串号）")
	}
}

func TestDeriveKeyImageURLChangeDoesNotBreakStability(t *testing.T) {
	// 同一张图的 URL 逐轮变化是真实场景；内容摘要入键会让键漂移。
	t1 := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://x/1.png"}},{"type":"text","text":"看图"}]}]}`
	t2 := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://x/2.png?sign=abc"}},{"type":"text","text":"看图"}]}]}`
	k1, k2 := ExtractKey([]byte(t1)), ExtractKey([]byte(t2))
	if k1 == "" {
		t.Fatal("含图片的首条消息也应能派生键（首图会话不能是粘性盲区）")
	}
	if k1 != k2 {
		t.Fatalf("图片 URL 变化导致派生键漂移：%q → %q", k1, k2)
	}
}

func TestDeriveKeyFirstImageOnlyConversation(t *testing.T) {
	// 纯图片首条消息（无 text part）：纯文本提取会得到空串 → 粘性盲区。
	// 签名口径下应能派生出非空键。
	body := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://x/1.png"}}]}]}`
	if k := ExtractKey([]byte(body)); k == "" {
		t.Fatal("纯图片首条消息应能派生键")
	}
}

func TestExtractKeyDegradesGracefully(t *testing.T) {
	// 绝不失败、永不 panic：各种畸形输入都返回空串（调用方回落到普通轮换）。
	cases := []string{
		``, `{}`, `not json`, `[]`, `null`,
		`{"messages":[]}`, `{"messages":"oops"}`,
		`{"messages":[{"role":"user"}]}`,
		`{"messages":[{"role":"user","content":null}]}`,
		`{"metadata":"oops"}`,
	}
	for _, b := range cases {
		if got := ExtractKey([]byte(b)); got != "" {
			t.Errorf("畸形输入 %q 应返回空串，实际 %q", b, got)
		}
	}
}

func TestDerivedKeyNamespaced(t *testing.T) {
	// 派生键带 "d-" 前缀，与显式会话 id 命名空间隔离。
	// 否则客户端恰好传了形如 "d-<hex>" 的 id 会与派生键混淆。
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	k := ExtractKey([]byte(body))
	if len(k) < 2 || k[:2] != derivedKeyPrefix {
		t.Fatalf("派生键应带 %q 前缀，实际 %q", derivedKeyPrefix, k)
	}
	// 显式 id 原样返回（不加前缀）
	if got := ExtractKey([]byte(`{"conversation_id":"d-abc"}`)); got != "d-abc" {
		t.Errorf("显式 id 应原样返回，实际 %q", got)
	}
}

func TestHashIndexDeterministic(t *testing.T) {
	// 同一 key 必须恒定映射到同一索引（否则重启后全部粘性丢失）。
	k := "conv-stable"
	first := hashIndex(k, 7)
	for i := 0; i < 50; i++ {
		if hashIndex(k, 7) != first {
			t.Fatal("hashIndex 不稳定")
		}
	}
	if hashIndex(k, 0) != 0 {
		t.Error("n=0 应返回 0（防除零）")
	}
}
