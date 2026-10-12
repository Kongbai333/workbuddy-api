// Package pool 的挑号策略（config.strategy 的执行侧）。
//
// 三种策略的取舍：
//
//   - credits（优先积分）：总是用余额最多的账号。积分集中消耗，账号闲置时不掉分。
//   - expire（临期优先，**默认**）：先把快过期的积分烧掉，避免到期作废。
//     这是历史默认行为，保留为默认以免升级即改变选号结果。
//   - roundrobin（负载均衡）：按 UID 排序轮流用，最大限度摊平消耗速度。
//
// 为什么需要可配置：临期优先只在「有人临期」时才有意义。若所有账号都很充裕
// （或都不区分到期日，如 Qoder 不下发到期时间），它退化为纯积分优先，与
// credits 完全同效；此时用户可能反而想要 roundrobin 来保护单账号不被连续扣分。
package pool

import "strings"

// Strategy 账号挑选策略。
type Strategy string

const (
	// StrategyCredits 优先积分：可用余额最多者。
	StrategyCredits Strategy = "credits"
	// StrategyExpire 临期优先：临期额度多者先被消耗（= 基线硬编码行为，默认）。
	StrategyExpire Strategy = "expire"
	// StrategyRoundRobin 负载均衡：从上次选中账号的下一个开始轮流用。
	StrategyRoundRobin Strategy = "roundrobin"
)

// AllStrategies 全部合法策略（校验与前端下拉共用）。
var AllStrategies = []Strategy{StrategyCredits, StrategyExpire, StrategyRoundRobin}

// ParseStrategy 解析策略串。空串 → 默认策略；第二返回值 false 表示非法（已回落默认）。
func ParseStrategy(s string) (Strategy, bool) {
	v := Strategy(strings.ToLower(strings.TrimSpace(s)))
	if v == "" {
		return StrategyExpire, true
	}
	for _, k := range AllStrategies {
		if v == k {
			return v, true
		}
	}
	return StrategyExpire, false
}

// Label 策略中文名（日志与面板说明用）。
func (s Strategy) Label() string {
	switch s {
	case StrategyCredits:
		return "优先积分"
	case StrategyRoundRobin:
		return "负载均衡"
	default:
		return "临期优先"
	}
}

// Desc 策略一句话说明（面板 tooltip）。
func (s Strategy) Desc() string {
	switch s {
	case StrategyCredits:
		return "总是使用可用积分最多的账号"
	case StrategyRoundRobin:
		return "按账号顺序轮流使用，摊平消耗速度"
	default:
		return "优先消耗即将到期的积分，避免作废（默认）"
	}
}
