package provider

// modelsuffix.go 模型 ID 的倍率后缀。
//
// ## 为什么需要
//
// 面板费率表能显示 `x0.06`，但用户真正接触模型名的场合是**客户端的模型下拉框**
// （Cherry Studio / Claude Code / LobeChat 等），而那里只显示 `/v1/models` 返回的
// `id`。裸 id（`workbuddy/hy3`）看不出烧不烧积分，费率表又不可能一直开着。
//
// 故让 `/v1/models` 直接下发带倍率的名字：`workbuddy/hy3 (0.06x)`。
// 用户在客户端里一眼看到「这个模型只要 0.06 倍积分」，不用来回切面板。
//
// ## 为什么必须能被识别（而不是显示了就完）
//
// 带后缀的名字是**要能直接复制粘贴到客户端配置里**的 —— 那正是使用场景。
// 所以路由侧（server.runtimeForModel）必须把后缀剥掉再查上游，
// 否则用户粘进来就直接 400/404 了。故这里同时提供 Format 与 Strip 一对。
//
// ## 格式约定
//
//   - 形状：`channel/model (RATE x)`，如 `workbuddy/hy3 (0.06x)`；
//   - RATE 保留两位小数（与费率表一致，避免 `0.06x` / `0.06000001x` 抖动）；
//   - 免费模型 Rate=0 → `(0.00x)`，照常显示（用户需要知道「这个是免费的」）；
//   - 无定价（Explicit=false / 上游未返回）→ **不加后缀**，保持裸 id，
//     绝不用 0 或猜测值冒充倍率。

import (
	"fmt"
	"strconv"
	"strings"
)

// RateSuffixFormat 生成倍率后缀（含前导空格），如 " (0.06x)"。
// 非显式倍率（上游未返回）返回空串 —— 调用方据此保持裸 id。
func RateSuffixFormat(rate float64, explicit bool) string {
	if !explicit || rate < 0 {
		return ""
	}
	return fmt.Sprintf(" (%.2fx)", rate)
}

// StripRateSuffix 剥掉模型 ID 尾部的倍率后缀，返回 (纯模型名, 是否带过倍率)。
//
// 幂等：纯模型名原样返回（第二个返回值为 false）。
// 容忍内部多余空白：`workbuddy/hy3(0.06x)` 与 `workbuddy/hy3 (0.06x)` 都能识别，
// 因为用户可能手敲或从别处粘贴而丢了空格。
//
// 判定为后缀的条件（三者同时满足，避免误伤合法模型名）：
//  1. 以 ')' 结尾；
//  2. 去掉尾部 ')' 后缀后，剩余部分以 'x' 结尾（大小写不敏感）；
//  3. 'x' 之前是合法浮点数。
//
// 条件 3 是关键：模型名本身可以带括号（上游展示名常见），但带 `数字x` 的
// 括号不是模型名的合理形态，误判风险极低。
func StripRateSuffix(model string) (string, bool) {
	s := strings.TrimSpace(model)
	if !strings.HasSuffix(s, ")") {
		return model, false
	}
	// 找最后一个 '('：允许 `hy3 (0.06x)` 之外的形态如 `hy3(0.06x)`
	open := strings.LastIndex(s, "(")
	if open < 0 {
		return model, false
	}
	inner := strings.TrimSpace(s[open+1 : len(s)-1])
	if !strings.HasSuffix(strings.ToLower(inner), "x") {
		return model, false
	}
	num := strings.TrimSpace(inner[:len(inner)-1])
	if num == "" {
		return model, false
	}
	if _, err := strconv.ParseFloat(num, 64); err != nil {
		return model, false
	}
	// 剥掉后缀及其前面的空格；不剩内容说明模型名整体就是后缀，非法。
	base := strings.TrimSpace(s[:open])
	if base == "" {
		return model, false
	}
	return base, true
}

// RateOfModel 从模型 ID 尾部的倍率后缀解析出倍率（ok=false 表示没带后缀）。
// 用于把「用户粘进来的名字」还原成可展示的倍率（错误信息/日志用）。
func RateOfModel(model string) (float64, bool) {
	s := strings.TrimSpace(model)
	if !strings.HasSuffix(s, ")") {
		return 0, false
	}
	open := strings.LastIndex(s, "(")
	if open < 0 {
		return 0, false
	}
	inner := strings.TrimSpace(s[open+1 : len(s)-1])
	if !strings.HasSuffix(strings.ToLower(inner), "x") {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(inner[:len(inner)-1]), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
