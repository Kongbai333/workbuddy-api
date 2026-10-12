package provider

// modelsuffix_test.go 模型 ID 倍率后缀的回归测试。
//
// 核心不变量：**「/v1/models 下发的名字」必须能原样粘回来请求成功**。
// 这是一对函数（Format / StripRateSuffix）的存在理由，单独测任一侧都不够。

import "testing"

// TestRateSuffixFormat 后缀格式与「无定价不加后缀」的约定。
func TestRateSuffixFormat(t *testing.T) {
	cases := []struct {
		rate     float64
		explicit bool
		want     string
	}{
		{0.06, true, " (0.06x)"},
		{0, true, " (0.00x)"}, // 免费模型照常显示：用户需要知道「这个不烧积分」
		{1.83, true, " (1.83x)"},
		{0.06, false, ""}, // 上游未返回倍率 → 不加后缀，绝不用 0 冒充
		{-1, true, ""},    // 防御：负倍率视为无效
	}
	for _, c := range cases {
		if got := RateSuffixFormat(c.rate, c.explicit); got != c.want {
			t.Errorf("RateSuffixFormat(%v,%v)=%q want %q", c.rate, c.explicit, got, c.want)
		}
	}
}

// TestStripRateSuffixRoundTrip 往返一致：Format 产出的名字必须能被 Strip 还原。
//
// 这是整个特性的命门 —— 用户在客户端里复制带倍率的名字，粘贴回来必须还能路由。
func TestStripRateSuffixRoundTrip(t *testing.T) {
	models := []struct {
		base string
		rate float64
	}{
		{"workbuddy/hy3", 0.06},
		{"traework/glm-5.3-flash", 0.06},
		{"traecode/kimi-k3", 1.83},
		{"qodercn/deepseek", 0.15},
	}
	for _, m := range models {
		full := m.base + RateSuffixFormat(m.rate, true)
		got, had := StripRateSuffix(full)
		if !had || got != m.base {
			t.Errorf("往返失败：%q → %q(had=%v)，应还原为 %q", full, got, had, m.base)
		}
		// 幂等：再剥一次不应再改变
		again, had2 := StripRateSuffix(got)
		if had2 || again != m.base {
			t.Errorf("非幂等：%q 再剥得 %q(had=%v)", got, again, had2)
		}
	}
}

// TestStripRateSuffixToleratesMissingSpace 用户手敲或从别处粘贴可能丢空格。
func TestStripRateSuffixToleratesMissingSpace(t *testing.T) {
	for _, in := range []string{
		"workbuddy/hy3 (0.06x)",
		"workbuddy/hy3(0.06x)",
		"workbuddy/hy3  (0.06x)",
		"  workbuddy/hy3 (0.06x)  ",
		"workbuddy/hy3 (0.06X)", // 大写 X
	} {
		got, had := StripRateSuffix(in)
		if !had || got != "workbuddy/hy3" {
			t.Errorf("StripRateSuffix(%q)=%q(had=%v)，应得 workbuddy/hy3", in, got, had)
		}
	}
}

// TestStripRateSuffixKeepsLegitNames 不能误伤**本身带括号**的合法模型名。
//
// 误伤代价很高：合法模型名被剥掉后半截 → 路由到不存在的上游模型 → 400。
func TestStripRateSuffixKeepsLegitNames(t *testing.T) {
	for _, in := range []string{
		"workbuddy/hy3",
		"traework/glm-5.3",
		"qoder/model (preview)", // 括号里不是 数字x
		"workbuddy/model (v2)",
		"workbuddy/model (beta)",
		"workbuddy/model (x)",    // 没有数字
		"workbuddy/model (abcx)", // 不是浮点数
		"workbuddy/model()",      // 空内容
		"workbuddy/model (0.06x", // 缺右括号
		"(0.06x)",                // 整体就是后缀 → 非法
	} {
		got, had := StripRateSuffix(in)
		if had {
			t.Errorf("误判 %q 带后缀（得到 %q）", in, got)
		}
		if got != in {
			t.Errorf("%q 被改动为 %q，裸名必须原样返回", in, got)
		}
	}
}

// TestRateOfModel 解析后缀里的倍率数值。
func TestRateOfModel(t *testing.T) {
	if r, ok := RateOfModel("workbuddy/hy3 (0.06x)"); !ok || r != 0.06 {
		t.Errorf("RateOfModel=(%v,%v)，want (0.06,true)", r, ok)
	}
	if r, ok := RateOfModel("workbuddy/hy3"); ok || r != 0 {
		t.Errorf("裸名不应解析出倍率，得到 (%v,%v)", r, ok)
	}
	if r, ok := RateOfModel("workbuddy/hy3 (free)"); ok {
		t.Errorf("非数字后缀不应解析，得到 %v", r)
	}
}

// TestStripRateSuffixNoChannelPrefixEdge 边界情形。
func TestStripRateSuffixNoChannelPrefixEdge(t *testing.T) {
	// 无 channel 前缀也要能剥（剥除只认最后一个 '(' 与尾部 ')'，与斜杠无关）
	for _, c := range []struct{ in, want string }{
		{"hy3 (0.06x)", "hy3"},
		{"a/b (0.06x)", "a/b"},
	} {
		got, had := StripRateSuffix(c.in)
		if !had || got != c.want {
			t.Errorf("StripRateSuffix(%q)=%q(had=%v)，want %q/true", c.in, got, had, c.want)
		}
	}
	// 整体就是后缀（前导空格 + 无 base）→ 剥完是空名，属非法，原样返回。
	// 不能返回空串：空模型名会让 runtimeForModel 的报错信息变得难以理解。
	if got, had := StripRateSuffix(" (0.06x)"); had || got != " (0.06x)" {
		t.Errorf("纯后缀应原样返回，得到 %q(had=%v)", got, had)
	}
}
