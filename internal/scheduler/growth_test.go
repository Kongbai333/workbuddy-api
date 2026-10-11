package scheduler

// 成长中心自动化的调度侧测试。
//
// 最重要的护栏：**开关关闭时零网络请求**。
// 这两项自动化会真实改变账号状态（兑换档位、消耗抽奖次数），
// 一旦「默认关闭」失效，用户会在不知情的情况下被消耗资源。
//
// 全部用例不发真实网络请求。

import (
	"testing"
	"time"

	"workbuddy-api/internal/auth"
	"workbuddy-api/internal/pool"
	"workbuddy-api/internal/provider"
	"workbuddy-api/internal/upstream"
)

// growthStub 嵌入 upstream.Client 以满足 provider.Upstream，
// 并覆盖成长方法以记录调用情况（不触网）。
type growthStub struct {
	upstream.Client
	redeemCalls  int
	travelCalls  int
	lastLocation int
	redeemErr    error
	travelErr    error
}

func (g *growthStub) GrowthRedeemAndDraw(a *auth.Auth) (int, int, string, error) {
	g.redeemCalls++
	return 1, 2, "stub-redeem", g.redeemErr
}

func (g *growthStub) GrowthTravelOnce(a *auth.Auth, locationID int) (string, error) {
	g.travelCalls++
	g.lastLocation = locationID
	return "stub-travel", g.travelErr
}

// plainUpstream **只暴露 provider.Upstream 的方法集** —— 通过嵌入接口实现。
//
// 目的：模拟「该渠道不支持成长中心」。若直接嵌入 upstream.Client，
// 成长方法会被一并提升，就测不出「不支持时静默跳过」这条路径了。
type plainUpstream struct{ provider.Upstream }

// newGrowthScheduler 构造一个带 1 个账号的调度器。
func newGrowthScheduler(up provider.Upstream, cfg Config) *Scheduler {
	p := pool.New("")
	p.Add(&auth.Auth{
		Kind: "workbuddy", UID: "u1", RefreshToken: "rt", AccessToken: "at",
		ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
	})
	cfg.Pool = p
	cfg.Upstream = up
	if cfg.Name == "" {
		cfg.Name = "test"
	}
	return New(cfg)
}

// ★ 核心护栏：开关全关时不得发起任何成长请求。
// 这是「默认关闭」承诺的机器验证 —— 靠人看代码容易漏。
func TestGrowthDisabledIsNoop(t *testing.T) {
	stub := &growthStub{}
	s := newGrowthScheduler(stub, Config{}) // 所有 growth 开关零值 = 关闭

	s.RunGrowthNow()

	if stub.redeemCalls != 0 {
		t.Fatalf("开关关闭却调用了兑换 %d 次", stub.redeemCalls)
	}
	if stub.travelCalls != 0 {
		t.Fatalf("开关关闭却调用了旅行 %d 次", stub.travelCalls)
	}
}

// 只开兑换时，不应触碰旅行。
func TestGrowthRedeemOnlyDoesNotTouchTravel(t *testing.T) {
	stub := &growthStub{}
	s := newGrowthScheduler(stub, Config{GrowthRedeemStreak: true})

	s.RunGrowthNow()

	if stub.redeemCalls != 1 {
		t.Fatalf("兑换调用 %d 次 want 1", stub.redeemCalls)
	}
	if stub.travelCalls != 0 {
		t.Fatalf("旅行开关关闭却调用了 %d 次", stub.travelCalls)
	}
}

// 只开旅行时，不应触碰兑换。
func TestGrowthTravelOnlyDoesNotTouchRedeem(t *testing.T) {
	stub := &growthStub{}
	s := newGrowthScheduler(stub, Config{GrowthTravel: true})

	s.RunGrowthNow()

	if stub.travelCalls != 1 {
		t.Fatalf("旅行调用 %d 次 want 1", stub.travelCalls)
	}
	if stub.redeemCalls != 0 {
		t.Fatalf("兑换开关关闭却调用了 %d 次", stub.redeemCalls)
	}
}

// 两项都开时，两个动作各执行一次（互不替代）。
func TestGrowthBothEnabled(t *testing.T) {
	stub := &growthStub{}
	s := newGrowthScheduler(stub, Config{GrowthRedeemStreak: true, GrowthTravel: true})

	s.RunGrowthNow()

	if stub.redeemCalls != 1 || stub.travelCalls != 1 {
		t.Fatalf("redeem=%d travel=%d want 1/1", stub.redeemCalls, stub.travelCalls)
	}
}

// 渠道不实现成长接口时必须静默跳过，且不得 panic。
// 现实场景：traework / qoder 等渠道共用同一套调度器代码。
func TestGrowthNonGrowthChannelSkips(t *testing.T) {
	plain := &plainUpstream{Upstream: &upstream.Client{}}
	s := newGrowthScheduler(plain, Config{GrowthRedeemStreak: true, GrowthTravel: true})

	// 能跑完即通过：重点是「不 panic、不误判为账号故障」
	s.RunGrowthNow()

	if s.growthAutomator() != nil {
		t.Fatal("plainUpstream 不应被识别为支持成长中心")
	}
}

// 停用账号必须跳过 —— 用户停用某账号后不应仍被动消耗其额度。
func TestGrowthSkipsDisabledAccounts(t *testing.T) {
	stub := &growthStub{}
	s := newGrowthScheduler(stub, Config{GrowthRedeemStreak: true, GrowthTravel: true})
	s.cfg.Pool.Disable("u1", "test")

	s.RunGrowthNow()

	if stub.redeemCalls != 0 || stub.travelCalls != 0 {
		t.Fatalf("停用账号仍被处理：redeem=%d travel=%d", stub.redeemCalls, stub.travelCalls)
	}
}

// 多个账号应逐个处理（不遗漏、不重复）。
func TestGrowthIteratesAllAccounts(t *testing.T) {
	stub := &growthStub{}
	p := pool.New("")
	for _, uid := range []string{"u1", "u2", "u3"} {
		p.Add(&auth.Auth{
			Kind: "workbuddy", UID: uid, RefreshToken: "rt", AccessToken: "at",
			ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
		})
	}
	s := New(Config{Pool: p, Upstream: stub, Name: "test", GrowthRedeemStreak: true})

	s.RunGrowthNow()

	if stub.redeemCalls != 3 {
		t.Fatalf("3 个账号应调用 3 次，实际 %d", stub.redeemCalls)
	}
}

// 旅行目的地越界或缺失时收敛为 1（上游只接受 1~4）。
func TestTravelLocationClamp(t *testing.T) {
	cases := []struct {
		cfg  int
		want int
	}{{0, 1}, {-5, 1}, {1, 1}, {2, 2}, {4, 4}, {5, 1}, {99, 1}}
	for _, c := range cases {
		stub := &growthStub{}
		s := newGrowthScheduler(stub, Config{GrowthTravel: true, GrowthTravelLocation: c.cfg})
		s.RunGrowthNow()
		if stub.lastLocation != c.want {
			t.Errorf("配置 %d → 传给上游 %d want %d", c.cfg, stub.lastLocation, c.want)
		}
	}
}

// 单个账号失败不得中断整批（下一个账号仍要处理）。
func TestGrowthOneAccountFailureDoesNotStopOthers(t *testing.T) {
	stub := &growthStub{redeemErr: errFakeGrowth{}}
	p := pool.New("")
	for _, uid := range []string{"u1", "u2"} {
		p.Add(&auth.Auth{
			Kind: "workbuddy", UID: uid, RefreshToken: "rt", AccessToken: "at",
			ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
		})
	}
	s := New(Config{Pool: p, Upstream: stub, Name: "test", GrowthRedeemStreak: true})

	s.RunGrowthNow()

	if stub.redeemCalls != 2 {
		t.Fatalf("首个账号失败后应继续处理第二个：calls=%d want 2", stub.redeemCalls)
	}
}

// 旅行失败同样不应影响兑换（两个动作相互独立）。
func TestGrowthTravelFailureDoesNotAffectRedeem(t *testing.T) {
	stub := &growthStub{travelErr: errFakeGrowth{}}
	s := newGrowthScheduler(stub, Config{GrowthRedeemStreak: true, GrowthTravel: true})

	s.RunGrowthNow()

	if stub.redeemCalls != 1 {
		t.Fatalf("旅行失败不应影响兑换：redeem=%d want 1", stub.redeemCalls)
	}
}

// runGrowthOncePerDay 当日只执行一次（防止窗口内每分钟重复触发）。
func TestRunGrowthOncePerDayMarksDay(t *testing.T) {
	stub := &growthStub{}
	s := newGrowthScheduler(stub, Config{GrowthRedeemStreak: true})

	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.Local)
	s.runGrowthOncePerDay(now)
	waitCalls(t, &stub.redeemCalls, 1)

	// 同一天再次触发：应被标记拦住，不再执行
	s.runGrowthOncePerDay(now.Add(time.Hour))
	time.Sleep(200 * time.Millisecond)
	if stub.redeemCalls != 1 {
		t.Fatalf("同日第二次触发不应执行：calls=%d want 1", stub.redeemCalls)
	}

	// 次日：允许再次执行
	s.runGrowthOncePerDay(now.AddDate(0, 0, 1))
	waitCalls(t, &stub.redeemCalls, 2)
}

// 未启用时 runGrowthOncePerDay 不应设置标记
// （否则用户当天中途开启会被误跳过）。
func TestRunGrowthOncePerDayNoopWhenDisabled(t *testing.T) {
	stub := &growthStub{}
	s := newGrowthScheduler(stub, Config{}) // 全关

	s.runGrowthOncePerDay(time.Date(2026, 10, 11, 9, 0, 0, 0, time.Local))

	if s.growthDoneDay != "" {
		t.Fatalf("未启用时不应设置当日标记，得 %q", s.growthDoneDay)
	}
	if stub.redeemCalls != 0 {
		t.Fatalf("未启用却执行了 %d 次", stub.redeemCalls)
	}
}

// waitCalls 轮询等待异步执行完成（避免固定 sleep 造成偶发失败）。
func waitCalls(t *testing.T, counter *int, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for *counter < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if *counter != want {
		t.Fatalf("等待调用次数失败：得 %d want %d", *counter, want)
	}
}

type errFakeGrowth struct{}

func (errFakeGrowth) Error() string { return "fake growth error" }
