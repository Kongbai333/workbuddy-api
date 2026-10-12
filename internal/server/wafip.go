package server

// wafip.go WAF IP 级拦截 fail-fast。
//
// ## 背景
//
// provider.ErrWafBlock 是 403 + 非业务信封（WAF 拦截页/空体）。既有处理是
// **账号级**软冷却 + 继续轮转。但实测（第三方 fork-scan-absorb）发现：
// **WAF 403 拦的是网关出口 IP，不是账号** —— 3 个账号 1 秒内全部 403。
//
// 账号级冷却在这种情况下是错的：轮转会把一次客户端请求放大 MaxRotate 倍，
// 同一个出口 IP 继续打上游只会**加重风控**，而且不可能成功（号都是好的，
// 是 IP 被拦了）。
//
// ## 判定
//
// ErrWafBlock 基础上的短窗多号计数：`wafIPWindow` 滑动窗内 ≥ `wafIPThreshold`
// 个**不同** UID 接连命中 WAF 403 → 判定 IP 级拦截，激活至 now+wafIPWindow。
//
// **只数不同号**：单号反复 403 是账号级偶发（归软冷却管），永不触发 IP 判定。
//
// 激活期内的新命中**不续期**（保守：不做主动探测，靠窗口自然解除）。

import (
	"log"
	"sync"
	"time"
)

// wafIPWindow IP 级判定滑动窗 + 激活时长。var（而非 const）是便于测试注入短窗。
var wafIPWindow = 60 * time.Second

// wafIPThreshold 判定阈值：窗内不同 UID 数达到该值即激活。
//
// 取 2 =「多号」的最小定义：单号反复 403 永不触发；两个不同号在 60s 内接连被拦
// （同一出口 IP）已是 IP 级证据。取 2 比 3 更早止损，少放大一次轮转。
const wafIPThreshold = 2

// wafIPGate WAF IP 级拦截状态机（Handler 内嵌，零值可用）。
type wafIPGate struct {
	mu    sync.Mutex
	hits  map[string]time.Time // uid → 最近一次 WAF 403 时刻（判定窗内，惰性剪枝）
	until time.Time            // IP 级拦截激活截止；零值 = 未激活
}

// noteWaf 记一次某账号的 WAF 403，返回 IP 级拦截是否激活。
// 调用方据此 fail-fast 终止轮转（优先于普通轮转退避）。
//
//   - 已激活（now < until）：不续期、不记账 → true；
//   - 未激活：记 hits[uid]=now（同号重复命中覆盖不累计，判定口径是「不同号数」），
//     剪掉窗外的过期命中；不同 UID 数达阈值 → 激活到 now+window，
//     打一条 WARN 供观测，并**清空判定窗**（解除后需全新命中重新判定，不叠旧账）。
func (g *wafIPGate) noteWaf(uid string) bool {
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Before(g.until) {
		return true // 激活期内新命中：不续期（靠窗口自然解除）
	}
	if g.hits == nil {
		g.hits = map[string]time.Time{}
	}
	g.hits[uid] = now
	for u, t := range g.hits {
		if now.Sub(t) > wafIPWindow {
			delete(g.hits, u)
		}
	}
	if len(g.hits) >= wafIPThreshold {
		g.until = now.Add(wafIPWindow)
		log.Printf("WARN: [server] WAF IP 级拦截：%s 内 %d 个账号撞 WAF 403，轮转 fail-fast 至 %s（出口 IP 被拦，继续轮转会加重风控）",
			wafIPWindow, len(g.hits), g.until.Format(time.RFC3339))
		g.hits = map[string]time.Time{}
		return true
	}
	return false
}

// active 报告 IP 级拦截是否激活（错误文案区分 IP 级/账号级措辞用）。
func (g *wafIPGate) active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return time.Now().Before(g.until)
}
