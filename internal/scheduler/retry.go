package scheduler

// retry.go 限流延迟重试。
//
// ## 为什么需要它
//
// 签到撞上限流（TraeWork 9074「当前参与用户太多」、上游 5xx）时，`CheckinResult.Retryable`
// 会被置位，但**如果没有下文，这个标记等于没写**：`runCheckin` 只能靠「本时段未完成」
// 等到下一个配置的签到时刻（默认 09:00 / 21:00）才再试 —— 早上撞限流要干等到晚上 21:00。
// 实测 2026-10-12 09:00 就是这么丢了一次签到（当天 14:26 手动点击才补领成功）。
//
// 单次调用内的重试也不够：`traework.CheckinClaim` 内部最多换 8 个设备号、总耗时约 1 分钟，
// 高峰期可能持续更久。
//
// 故：可重试失败后由本文件安排**独立于定时点的延迟重试**（10/20/30… 分钟递增），
// Run 主循环把唤醒点提前到重试时刻。
//
// ## 与「窗口重试」（CheckinRetryUntil）的关系
//
// 两者互补，不重复：
//   - 窗口重试按**时段**展开（QoderCN 的 10:00–12:00 每分钟跑），解决
//     「活动尚未创建」—— 上游的节奏问题，与具体账号无关；
//   - 本文件按**账号**安排，解决「这个号被限流了」—— 同批次里别的号可能好好的，
//     不该拖着它们一起每分钟重试。
//
// ## 为什么必须按渠道开关（RetryOnFailure，默认关）
//
// 新项目的 `Retryable` 语义比旧项目**宽得多**：网络抖动、上游 5xx、`no_campaign`、
// 甚至 `no refresh token` 都会被置位（见 checkinOne）。旧项目只对
// `isRateLimited`（9074）安排重试。
//
// 若照搬「凡 Retryable 就重试」，会出现两类无意义重试：
//   - `no_campaign`：Qoder 活动还没创建 —— 那是**时段**问题，窗口重试已在处理，
//     再叠一层按账号的延迟重试纯属双重重试；
//   - `no refresh token`：只能靠用户重新登录，重试多少次都不会好。
//
// 故默认关闭，由 main.go 针对确有 9074 限流的渠道（TraeWork）显式打开。

import (
	"log"
	"sync"
	"time"
)

const (
	// rateLimitRetryDelay 限流后的首次延迟重试间隔。
	// 10 分钟：足够跨过一波瞬时高峰，又不至于让用户等太久。
	rateLimitRetryDelay = 10 * time.Minute

	// rateLimitRetryMax 单账号每天最多安排多少次延迟重试。
	// 防止上游长时间异常时无限重试（每次重试都是一次真实的上游请求）。
	rateLimitRetryMax = 6
)

// retryState 单账号的限流重试状态（当天有效，跨天自动重置）。
type retryState struct {
	count   int       // 当天已安排的延迟重试次数
	lastDay int       // 归属日期（YearDay），与 count 一起用于跨天重置
	nextAt  time.Time // 下次重试时刻
	pending bool      // 是否有待执行的重试
	// slot 发起这次重试的签到时段（当天分钟数）；-1 = 无归属
	// （面板手动签到 / RunCheckinNow 触发的重试没有时段概念）。
	//
	// 为什么必须记：延迟重试成功后要把原时段标记为「当日完成」，
	// 否则 fireMinutes 会一直把该时段算作未完成 —— 对开了
	// CheckinRetryUntil 的渠道，这会在成功之后仍每分钟重试到窗口结束。
	slot int
}

// retryFlag 是延迟重试的开关。
//
// 独立于 retries 状态用一把锁：retries 的方法内部要取 retryMu，
// 若开关也用它会在「读开关 → 改状态」的路径上自锁。
type retryFlag struct {
	mu sync.RWMutex
	on bool
}

func (f *retryFlag) get() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.on
}

func (f *retryFlag) set(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.on = on
}

// markRetryable 为某账号安排一次延迟重试，返回 (下次重试时刻, 是否还能安排)。
//
// slot 为发起本次签到的时段（-1 = 无归属，见 retryState.slot）。
//
// 退避递增：10min → 20min → 30min …（线性而非指数 —— 指数很快冲到小时级，
// 那样就失去了「尽快补领」的意义）。跨天把计数清零（限流状态不该隔夜累积）。
// 超过 rateLimitRetryMax 返回 ok=false：当天不再安排，交给下一个签到时段。
func (s *Scheduler) markRetryable(uid string, now time.Time, slot int) (time.Time, bool) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	st := s.retries[uid]
	day := now.YearDay()
	if st == nil || st.lastDay != day {
		st = &retryState{lastDay: day, slot: slot}
		s.retries[uid] = st
	}
	if st.count >= rateLimitRetryMax {
		return time.Time{}, false
	}
	st.count++
	st.nextAt = now.Add(rateLimitRetryDelay * time.Duration(st.count))
	st.pending = true
	// 续排时保留**最初的**时段归属：同一账号一整天都属同一个时段序列，
	// 取最后一次传入的值会随重试链漂移（面板手动签到的 slot=-1 也不该覆盖它）。
	if slot >= 0 {
		st.slot = slot
	}
	return st.nextAt, true
}

// retryTarget 读某账号的重试所归属的时段，以及该重试是否仍在**当天**。
// 第二个返回值为 false 表示重试跨了零点（排期在上一天、执行在今天），
// 此时不得回填时段完成标记（见 runDueRetries 的说明）。
// 无重试记录时返回 (-1, true)。
func (s *Scheduler) retryTarget(uid string, now time.Time) (int, bool) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	st, ok := s.retries[uid]
	if !ok {
		return -1, true
	}
	// 跨天重排会把 lastDay 刷成当天，所以 lastDay != 当天 才是真正的跨零点。
	return st.slot, st.lastDay == now.YearDay()
}

// dueRetries 取出已到期的重试账号，并清除其 pending 标记。
func (s *Scheduler) dueRetries(now time.Time) []string {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	var out []string
	for uid, st := range s.retries {
		if st.pending && !now.Before(st.nextAt) {
			st.pending = false
			out = append(out, uid)
		}
	}
	return out
}

// nextRetryAt 返回最近一次待执行重试的时刻（无则零值）。
// Run 主循环用它把唤醒点提前到「重试时刻」而不是「下一个定时签到」。
func (s *Scheduler) nextRetryAt() time.Time {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	var best time.Time
	for _, st := range s.retries {
		if !st.pending {
			continue
		}
		if best.IsZero() || st.nextAt.Before(best) {
			best = st.nextAt
		}
	}
	return best
}

// clearRetry 清除某账号的重试状态（签到成功 / 账号被删 / 不再可重试时调用）。
func (s *Scheduler) clearRetry(uid string) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	delete(s.retries, uid)
}

// pendingRetryCount 当前待执行的重试账号数（测试与诊断用）。
func (s *Scheduler) pendingRetryCount() int {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	n := 0
	for _, st := range s.retries {
		if st.pending {
			n++
		}
	}
	return n
}

// runDueRetries 对已到期的账号单独重试签到（只签这些号，不动其它账号）。
//
// 结果决定去向：成功 → 清除；仍可重试 → 退避继续递增后再安排一次；
// 不可重试（session dead 等）→ 清除，交给账号惩罚逻辑，避免无意义地打上游。
func (s *Scheduler) runDueRetries(now time.Time) {
	if !s.retryOnFailureEnabled() {
		return // 渠道未开启：连状态检查都省掉
	}
	uids := s.dueRetries(now)
	if len(uids) == 0 {
		return
	}
	name := s.name()
	log.Printf("checkin retry start platform=%s accounts=%d uids=%v", name, len(uids), uids)
	// 本轮结束时，若已没有待执行的重试，且某个时段名下有账号在此轮补领成功，
	// 就把该时段标记为「当日完成」—— 否则 fireMinutes 会一直认为它没完成。
	//
	// 跨零点的重试必须跳过：markSlotDone 用 time.Now() 拼 key，而退避最长可累计到
	// 10+20+…+60 分钟。23:50 的签到失败、跨零点后才补领成功时，直接标记会把
	// **第二天**同一时刻标成「当日完成」，第二天到点被 fireMinutes 跳过 —— 整整
	// 漏签一天且毫无日志。判据是 retryState.lastDay（重试排期那天）是否等于当天。
	doneSlots := map[int]bool{}
	for _, uid := range uids {
		if s.cfg.Pool.AuthByUID(uid) == nil {
			s.clearRetry(uid) // 账号已从池中消失
			continue
		}
		slot, sameDay := s.retryTarget(uid, now)
		r := s.checkinOne(uid, slot)
		switch {
		case r.OK:
			if slot >= 0 && sameDay {
				doneSlots[slot] = true
			} else if slot >= 0 {
				log.Printf("checkin retry crossed midnight platform=%s uid=%s slot=%d（跨零点，不回填时段标记以免误标次日）",
					name, uid, slot)
			}
			s.clearRetry(uid)
			log.Printf("checkin retry ok platform=%s uid=%s", name, uid)
		case r.Retryable:
			if at, ok := s.markRetryable(uid, now, slot); ok {
				log.Printf("checkin retry scheduled platform=%s uid=%s at=%s",
					name, uid, at.Format("15:04:05"))
			} else {
				log.Printf("checkin retry exhausted-for-today platform=%s uid=%s", name, uid)
			}
		default:
			s.clearRetry(uid)
			log.Printf("checkin retry aborted platform=%s uid=%s msg=%s", name, uid, r.Msg)
		}
	}
	// 仅当该时段已无任何待执行重试时才标记完成：
	// 同批次里可能有别的号仍在重试链上，提前标记会让它们被跳过。
	if s.pendingRetryCount() == 0 {
		for slot := range doneSlots {
			s.markSlotDone(slot)
			log.Printf("checkin slot completed by retry platform=%s slot=%d", name, slot)
		}
	}
}

// scheduleRetryFor 签到后的统一钩子：按结果维护延迟重试状态。
//
// slot 为本次签到所属的时段（-1 = 面板手动签到等无时段场景），
// 会被记进 retryState，供重试成功后回填该时段的「当日完成」标记。
//
// 只在渠道开启 RetryOnFailure 时动作。成功即清除（避免第二天残留昨天的退避计数），
// 可重试失败则安排/续排一次。
func (s *Scheduler) scheduleRetryFor(uid string, r CheckinResult, now time.Time, slot int) {
	if !s.retryOnFailureEnabled() {
		return
	}
	name := s.name()
	if r.OK || !r.Retryable {
		if s.pendingRetryFor(uid) {
			s.clearRetry(uid)
		}
		return
	}
	at, ok := s.markRetryable(uid, now, slot)
	if !ok {
		log.Printf("checkin retry exhausted-for-today platform=%s uid=%s", name, uid)
		return
	}
	log.Printf("checkin retry scheduled platform=%s uid=%s at=%s",
		name, uid, at.Format("15:04:05"))
}

// pendingRetryFor 该账号当前是否有待执行的重试。
func (s *Scheduler) pendingRetryFor(uid string) bool {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	st, ok := s.retries[uid]
	return ok && st.pending
}

// retryOnFailureEnabled 读当前延迟重试开关。
func (s *Scheduler) retryOnFailureEnabled() bool { return s.retryFlag.get() }

// setRetryOnFailure 运行中切换开关。
//
// 不导出：本项目的延迟重试开关与 CheckinRetryUntil 一样是**构造期决定**的
// （main.go 按渠道装配），面板不提供改动入口 —— 导出没有调用方的公开方法
// 等于对外承诺一个不存在的功能。留作小写以便包内测试覆盖。
func (s *Scheduler) setRetryOnFailure(on bool) { s.retryFlag.set(on) }
