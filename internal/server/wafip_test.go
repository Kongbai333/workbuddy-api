package server

// wafip_test.go WAF IP 级拦截判定的回归测试。
//
// 核心不变量：**只数不同账号**。单号反复 403 是账号级偶发（归软冷却管），
// 绝不能升级成 IP 级 fail-fast —— 否则一个坏号就会把整个出口 IP 停掉。

import (
	"testing"
	"time"
)

// withWafWindow 临时缩短判定窗（生产恒 60s，测试要秒级完成）。
func withWafWindow(t *testing.T, d time.Duration) {
	t.Helper()
	old := wafIPWindow
	wafIPWindow = d
	t.Cleanup(func() { wafIPWindow = old })
}

// TestSingleAccountNeverEscalates 单号反复 403 永不触发 IP 判定。
func TestSingleAccountNeverEscalates(t *testing.T) {
	withWafWindow(t, 50*time.Millisecond)
	var g wafIPGate
	for i := 0; i < 10; i++ {
		if g.noteWaf("only-one") {
			t.Fatalf("第%d 次单号 403 就升级成 IP 级了（会把一个坏号停成整个出口 IP）", i+1)
		}
	}
	if g.active() {
		t.Fatal("单号不该激活 IP 级拦截")
	}
}

// TestTwoAccountsEscalates 两个不同号在窗内接连 403 → 判 IP 级。
func TestTwoAccountsEscalates(t *testing.T) {
	withWafWindow(t, time.Second)
	var g wafIPGate
	if g.noteWaf("a") {
		t.Fatal("第一个号不该触发（还没到多号证据）")
	}
	if !g.noteWaf("b") {
		t.Fatal("第二个不同号在窗内命中应升级为 IP 级")
	}
	if !g.active() {
		t.Fatal("active() 应为 true")
	}
}

// TestOutOfWindowDoesNotEscalate 超出判定窗的旧命中不计入。
func TestOutOfWindowDoesNotEscalate(t *testing.T) {
	withWafWindow(t, 20*time.Millisecond)
	var g wafIPGate
	g.noteWaf("a")
	time.Sleep(40 * time.Millisecond) // a 的命中滑出窗口
	if g.noteWaf("b") {
		t.Fatal("窗外旧命中不应计入判定（否则慢速请求会误判 IP 级）")
	}
}

// TestActiveHitsDoNotExtendWindow 激活期内的新命中**不续期**
// （保守：不做主动探测，靠窗口自然解除）。
func TestActiveHitsDoNotExtendWindow(t *testing.T) {
	withWafWindow(t, 30*time.Millisecond)
	var g wafIPGate
	g.noteWaf("a")
	g.noteWaf("b") // 激活，until = now+30ms
	firstUntil := g.until

	time.Sleep(10 * time.Millisecond)
	g.noteWaf("c") // 激活期内：应返回 true 但**不**改 until
	if g.until != firstUntil {
		t.Error("激活期内的命中不应续期（until 被改动了）")
	}
	if !g.noteWaf("d") {
		t.Error("激活期内应恒返回 true")
	}
}

// TestWindowRearmsAfterExpiry 窗口过后需全新命中重新判定
// （激活时已清空 hits，故不会拿旧账直接二次激活）。
func TestWindowRearmsAfterExpiry(t *testing.T) {
	withWafWindow(t, 20*time.Millisecond)
	var g wafIPGate
	g.noteWaf("a")
	g.noteWaf("b") // 激活；hits 已被清空
	time.Sleep(40 * time.Millisecond)
	if g.active() {
		t.Fatal("窗口过后应自然解除")
	}
	// 解除后只有单号命中 → 不应立刻再激活（hits 已清空，不能叠旧账）
	if g.noteWaf("c") {
		t.Fatal("解除后单号命中不应立即再激活")
	}
	// 再来一个不同号才重新激活
	if !g.noteWaf("d") {
		t.Fatal("全新两号应重新激活")
	}
}

// TestGateZeroValueUsable 零值 Gate 直接可用（Handler 内嵌，不需显式构造）。
func TestGateZeroValueUsable(t *testing.T) {
	withWafWindow(t, time.Second)
	var g wafIPGate
	if g.active() {
		t.Fatal("零值不应激活")
	}
	if g.noteWaf("a") {
		t.Fatal("零值首个命中不应激活")
	}
}
