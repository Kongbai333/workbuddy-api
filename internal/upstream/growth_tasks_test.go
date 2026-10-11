package upstream

// 成长任务编排层测试。
//
// 三类护栏：
//  1. **DryRun 零写请求** —— 「先试运行看清楚再动手」是这个功能的安全阀，
//     一旦失效，用户点「试运行」也会真实改账号状态。
//  2. **事件形状** —— 字段名写错时上游仍返回 200（只是不入账），
//     所以必须靠断言载荷结构来兜住这类「静默失败」。
//  3. **动作表与任务码的对应** —— 覆盖不到的任务会静默变成「需手动」，
//     用户会以为功能缺失。
//
// 全部用例不发起真实网络请求（httptest 除外，它只走本机回环）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 把节流压到 0，避免用例为等间隔白跑几秒。
func fastGaps(t *testing.T) {
	t.Helper()
	oldGap, oldMP, oldAction := growthTaskGap, growthTaskMPChatGap, growthTaskMPActionGap
	growthTaskGap, growthTaskMPChatGap, growthTaskMPActionGap = 0, 0, 0
	t.Cleanup(func() {
		growthTaskGap, growthTaskMPChatGap, growthTaskMPActionGap = oldGap, oldMP, oldAction
	})
}

// --- 1. 动作表与任务码 ---

func TestActionForKnownCodes(t *testing.T) {
	codes := []string{
		"chat_5", "first_buddy", "RichMeow_Chat", "Buddy_App", "Buddy_App_QQ",
		"automation_1", "Library_read", "template_5", "playbook_prompt",
		"create_canvas", "Hp_Appearance", "skill_1", "expert_5",
		"Expert_team_use_3", "Expert_lighthouse", "black_cat",
		"school_season", "Sequential_Tasks_1", "Sequential_Tasks_2",
		"Sequential_Tasks_3", "Sequential_Tasks_4", "Sequential_Tasks_5",
		"Sequential_Tasks_6", "Sequential_Tasks_7",
	}
	for _, code := range codes {
		if actionFor(code) == nil {
			t.Errorf("任务 %s 无对应动作（会静默降级为「需手动」）", code)
		}
	}
	// 每个动作用例都必须有 Run（否则执行时空指针 panic）。
	for _, a := range growthTaskActions {
		if a.Run == nil {
			t.Errorf("动作 %s 未实现 Run", a.Code)
		}
		if a.Desc == "" {
			t.Errorf("动作 %s 缺少说明（面板会显示空白）", a.Code)
		}
	}
}

func TestActionForModelChatPrefix(t *testing.T) {
	if got := actionFor("Model_chat_GLM5.2"); got == nil || got != &modelChatAction {
		t.Fatalf("Model_chat_GLM5.2 应走前缀动作，得到 %+v", got)
	}
	if got := actionFor("Model_chat_UnknownX"); got != &modelChatAction {
		t.Fatal("未知模型的任务码也应走前缀动作（内部再决定是否跳过）")
	}
	if actionFor("Expert_Philanthropy") != nil {
		t.Fatal("Expert_Philanthropy 需真实捐款回执，不应有动作")
	}
	if actionFor("完全不存在") != nil {
		t.Fatal("未知任务不应有动作")
	}
}

func TestModelFromTaskCode(t *testing.T) {
	id, name, ok := modelFromTaskCode("Model_chat_GLM5.2")
	if !ok || id != "glm-5.2" || name != "GLM-5.2" {
		t.Fatalf("GLM5.2 解析错误：%s/%s/%t", id, name, ok)
	}
	// 推导不出来的必须返回 false —— 宁可不动，也不要拿猜的模型名去请求。
	if _, _, ok := modelFromTaskCode("Model_chat_SomeNewModel"); ok {
		t.Fatal("未知模型名不应被猜出来")
	}
}

func TestMPTaskCodeClassification(t *testing.T) {
	for _, code := range []string{"school_season", "Sequential_Tasks_1", "Sequential_Tasks_7"} {
		if !isMPTaskCode(code) {
			t.Errorf("%s 应被判为小程序口径", code)
		}
	}
	for _, code := range []string{"chat_5", "first_buddy", "RichMeow_Chat", "Model_chat_GLM5.2"} {
		if isMPTaskCode(code) {
			t.Errorf("%s 不应被判为小程序口径", code)
		}
	}
	// 动作表里登记的 mp 任务必须与 mpTaskCodes 一致，否则查询/领奖会走错口径。
	for _, a := range growthTaskActions {
		if strings.HasPrefix(a.Code, "Sequential_") || a.Code == "school_season" {
			if !isMPTaskCode(a.Code) {
				t.Errorf("动作 %s 属于小程序口径，但未在 mpTaskCodes 登记（领奖会走错端点）", a.Code)
			}
		}
	}
}

// --- 2. 纯函数 ---

func TestInNightWindow(t *testing.T) {
	cases := []struct {
		hour int
		want bool
	}{
		{22, false}, {23, true}, {0, true}, {7, true}, {8, false}, {12, false},
	}
	for _, c := range cases {
		got := InNightWindow(time.Date(2026, 10, 12, c.hour, 30, 0, 0, time.Local))
		if got != c.want {
			t.Errorf("%d 时 → %t want %t", c.hour, got, c.want)
		}
	}
}

func TestProgressTextAndNeedFor(t *testing.T) {
	if got := progressText(Task{Current: 3, Target: 5}); got != "3/5" {
		t.Fatalf("progressText=%q", got)
	}
	if got := progressText(Task{Current: 7, Target: 0}); got != "7/?" {
		t.Fatalf("target 未知时应显示 7/?，得到 %q", got)
	}
	if got := needFor(Task{Current: 2, Target: 5}, 5); got != 3 {
		t.Fatalf("needFor=%d want 3", got)
	}
	// target 下发 0（未 accept 的 mp 任务）时必须用 fallback，否则会「0 差额」跳过。
	if got := needFor(Task{Current: 0, Target: 0}, 5); got != 5 {
		t.Fatalf("target=0 时应回退 fallback=5，得到 %d", got)
	}
	if got := needFor(Task{Current: 5, Target: 5}, 5); got != 0 {
		t.Fatalf("已达标应为 0，得到 %d", got)
	}
}

// --- 3. 事件链形状 ---

// eventCodes 提取事件链的 eventCode 序列。
func eventCodes(events []map[string]any) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		s, _ := e["eventCode"].(string)
		out = append(out, s)
	}
	return out
}

func TestDesktopChatSequenceShape(t *testing.T) {
	ev := DesktopChatSequence("conv-1", "req-1", "msg-1", "fast-model", "fast-model")
	want := []string{
		"agent_task_created", "chat_message_send", "chat_request_send",
		"chat_message_response", "chat_message_status", "chat_request_response",
	}
	got := eventCodes(ev)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("事件链顺序错误：\n got %v\nwant %v", got, want)
	}
	// 成功回执是这个任务被点亮的关键（上游会校验消息成功）。
	for _, e := range ev {
		if e["eventCode"] == "chat_message_response" {
			if e["isSuccessful"] != true {
				t.Fatal("chat_message_response 必须 isSuccessful=true，否则任务被视为失败对话")
			}
			if e["conversationId"] != "conv-1" {
				t.Fatal("chat_message_response 未携带 conversationId")
			}
		}
	}
}

func TestDesktopBuddyAppSequenceShape(t *testing.T) {
	ev := DesktopBuddyAppSequence(DesktopBuddyAppQQID, DesktopBuddyAppQQName)
	if len(ev) != 5 {
		t.Fatalf("buddyapp 应为 5 连事件，得到 %d", len(ev))
	}
	for _, e := range ev {
		if e["buddyId"] != DesktopBuddyAppQQID {
			t.Fatalf("事件 %v 缺少 buddyId", e["eventCode"])
		}
		if e["mode"] != "LOCAL" {
			t.Fatalf("事件 %v 的 mode 应为 LOCAL", e["eventCode"])
		}
	}
}

func TestDesktopTemplateUseSequenceShape(t *testing.T) {
	ev := DesktopTemplateUseSequence("conv-2", "req-2", "3", "竞品分析")
	if len(ev) != 8 { // 6 条 chat 链 + 2 条模板事件
		t.Fatalf("模板事件组长度=%d want 8", len(ev))
	}
	codes := eventCodes(ev)
	if codes[len(codes)-1] != "template_used" {
		t.Fatalf("末位应为 template_used，得到 %s", codes[len(codes)-1])
	}
	last := ev[len(ev)-1]
	if last["template_id"] != "3" {
		t.Fatal("template_used 未携带 template_id")
	}
	// agent_task_created_with_template 必须与同一个 requestId JOIN，
	// 否则上游会把模板事件与对话链视为两次独立行为。
	var found bool
	for _, e := range ev {
		if e["eventCode"] == "agent_task_created_with_template" {
			found = true
			if e["requestId"] != "req-2" {
				t.Fatal("模板创建事件未 JOIN 同一 requestId")
			}
		}
	}
	if !found {
		t.Fatal("缺少 agent_task_created_with_template")
	}
}

func TestDesktopPlaybookPromptSequenceShape(t *testing.T) {
	ev := DesktopPlaybookPromptSequence("conv-3", "req-3", "case-1", "案例名")
	codes := eventCodes(ev)
	joined := strings.Join(codes, ",")
	for _, want := range []string{"web_element_click", "playbook_cta_click", "playbook_prompt_send"} {
		if !strings.Contains(joined, want) {
			t.Errorf("缺少事件 %s", want)
		}
	}
	last := ev[len(ev)-1]
	if last["id"] != "case-1" || last["conversationId"] != "conv-3" {
		t.Fatal("playbook_prompt_send 未携带案例 id / conversationId")
	}
}

func TestMpChatEventActivityID(t *testing.T) {
	ev := MpChatEvent("conv-mp", "")
	if _, ok := ev["activityId"]; ok {
		t.Fatal("不带 activityId 的口径不应出现该字段")
	}
	ev2 := MpChatEvent("conv-mp", schoolOpenDayActivityID)
	if ev2["activityId"] != schoolOpenDayActivityID {
		t.Fatal("校园日口径必须带 activityId（缺了不点亮）")
	}
	if ev["agentName"] != "mp" {
		t.Fatalf("小程序事件 agentName 应为 mp，得到 %v", ev["agentName"])
	}
	if ev["codebuddy.session_id"] != "conv-mp" {
		t.Fatal("缺少 codebuddy.session_id 关联字段")
	}
}

func TestMpExpertUseEventShape(t *testing.T) {
	ev := MpExpertUseEvent("ex_abc", "某专家", "agent")
	// 与桌面口径是两套，勿混：小程序固定 2.2.8 + send_message，且不带 conversationId。
	if ev["extVersion"] != "2.2.8" {
		t.Fatalf("extVersion=%v want 2.2.8", ev["extVersion"])
	}
	if ev["type"] != "send_message" {
		t.Fatalf("type=%v want send_message", ev["type"])
	}
	if ev["source"] != "mini_program" {
		t.Fatalf("source=%v want mini_program", ev["source"])
	}
	if _, ok := ev["conversationId"]; ok {
		t.Fatal("小程序 expert_actual_use 不应带 conversationId")
	}
}

// --- 4. httptest 用例 ---

// taskListJSON 构造任务列表响应体。
func taskListJSON(tasks ...map[string]any) string {
	raw, _ := json.Marshal(map[string]any{
		"code": 0,
		"data": map[string]any{"tasks": tasks},
	})
	return string(raw)
}

// TestGrowthRunTasksDryRunIssuesNoWrites 是本文件最重要的护栏。
//
// 「试运行」的意义就是**不动账号**。若 DryRun 泄漏出写请求，
// 用户以为只是看看，实际已经消耗了抽奖次数/点亮了任务。
func TestGrowthRunTasksDryRunIssuesNoWrites(t *testing.T) {
	fastGaps(t)
	var posts, gets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		} else {
			gets++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(taskListJSON(
			map[string]any{"task_code": "chat_5", "title": "聊天 5 次",
				"progress": map[string]any{"current": 0, "target": 5}, "accept_status": "not_accepted"},
			map[string]any{"task_code": "first_buddy", "title": "领养",
				"progress": map[string]any{"current": 0, "target": 1}, "accept_status": "not_accepted"},
			// 该任务无动作且已达标 —— DryRun 下也不得去领奖。
			map[string]any{"task_code": "SomeManualTask", "title": "手动任务",
				"progress": map[string]any{"current": 1, "target": 1}},
		)))
	}))
	defer srv.Close()

	c := New()
	c.ChatBaseCN, c.BillingBaseCN, c.WebBaseCN = srv.URL, srv.URL, srv.URL

	rep := c.GrowthRunTasks(testAuth(), GrowthTaskOptions{DryRun: true})

	if posts != 0 {
		t.Fatalf("DryRun 下出现了 %d 个 POST —— 试运行泄漏成了真实写操作", posts)
	}
	if gets == 0 {
		t.Fatal("DryRun 至少要读一次任务列表才能汇报进度")
	}
	if rep.Err != "" {
		t.Fatalf("DryRun 不应整体失败：%s", rep.Err)
	}
	// 每个可执行任务都应记录为「跳过 + 试运行说明」。
	var dryRunRows int
	for _, o := range rep.Tasks {
		if o.Status == "skipped" && strings.Contains(o.Msg, "试运行") {
			dryRunRows++
		}
	}
	if dryRunRows < 2 {
		t.Fatalf("DryRun 应汇报每个动作的计划，实际只记录 %d 条", dryRunRows)
	}
}

// 已达标但无动作的任务应被直接领奖（这是「顺手把该拿的拿了」的路径）。
func TestGrowthRunTasksClaimsClaimableTask(t *testing.T) {
	fastGaps(t)
	var claimPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			claimPath = r.URL.Path
			_, _ = w.Write([]byte(`{"code":0,"data":{"already_claimed":false,"credit":300,"energy":8}}`))
			return
		}
		_, _ = w.Write([]byte(taskListJSON(
			map[string]any{"task_code": "SomeManualTask", "title": "手动任务",
				"progress": map[string]any{"current": 1, "target": 1}},
		)))
	}))
	defer srv.Close()

	c := New()
	c.ChatBaseCN, c.BillingBaseCN, c.WebBaseCN = srv.URL, srv.URL, srv.URL

	rep := c.GrowthRunTasks(testAuth(), GrowthTaskOptions{})

	if rep.Credit != 300 || rep.Energy != 8 {
		t.Fatalf("未计入领奖结果：credit=%d energy=%d", rep.Credit, rep.Energy)
	}
	// 领奖必须走 Web 域的路径式端点（历史坑：走 CLI 域同名路径恒 400）。
	if claimPath != "/activity/growth/tasks/SomeManualTask/claim" {
		t.Fatalf("领奖路径错误：%q", claimPath)
	}
}

// 已领取的任务不得再次请求领奖（上游虽幂等，但无谓请求会浪费风控额度）。
func TestGrowthRunTasksSkipsClaimedTask(t *testing.T) {
	fastGaps(t)
	var claimHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			claimHits++
		}
		_, _ = w.Write([]byte(taskListJSON(
			map[string]any{"task_code": "SomeManualTask", "title": "已领",
				"progress":      map[string]any{"current": 1, "target": 1},
				"accept_status": "claimed"},
		)))
	}))
	defer srv.Close()

	c := New()
	c.ChatBaseCN, c.BillingBaseCN, c.WebBaseCN = srv.URL, srv.URL, srv.URL

	rep := c.GrowthRunTasks(testAuth(), GrowthTaskOptions{})

	if claimHits != 0 {
		t.Fatalf("已领取的任务仍发起了 %d 次领奖请求", claimHits)
	}
	// 已领取的任务**不**计入报告 —— 一轮下来十几项早已领完，
	// 全列出来只会把真正失败的那几行淹没。
	if len(rep.Tasks) != 0 {
		t.Fatalf("已领取的任务不应出现在报告里，得到 %+v", rep.Tasks)
	}
	if len(rep.Manual) != 0 {
		t.Fatalf("已领取的任务不应计入「需手动」，得到 %v", rep.Manual)
	}
}

// 任务列表 404（渠道不支持成长中心）应作为整体错误上报，
// 让调度器整轮中止而不是逐账号重试。
func TestGrowthRunTasksUnsupportedChannel(t *testing.T) {
	fastGaps(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"msg":"not found"}`))
	}))
	defer srv.Close()

	c := New()
	c.ChatBaseCN, c.BillingBaseCN, c.WebBaseCN = srv.URL, srv.URL, srv.URL

	rep := c.GrowthRunTasks(testAuth(), GrowthTaskOptions{})
	if rep.Err == "" {
		t.Fatal("404 应产生整体性错误（调度器据此中止整轮）")
	}
	if !IsGrowthUnsupported(stringsToErr(rep.Err)) {
		t.Fatalf("404 应被判为「渠道不支持」，得到 %q", rep.Err)
	}
}

// Only 过滤：只跑指定任务，未列入的不得出现在报告里。
func TestGrowthRunTasksOnlyFilter(t *testing.T) {
	fastGaps(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(taskListJSON(
			map[string]any{"task_code": "chat_5", "title": "a",
				"progress": map[string]any{"current": 1, "target": 5}},
			map[string]any{"task_code": "RichMeow_Chat", "title": "b",
				"progress": map[string]any{"current": 1, "target": 1}},
		)))
	}))
	defer srv.Close()

	c := New()
	c.ChatBaseCN, c.BillingBaseCN, c.WebBaseCN = srv.URL, srv.URL, srv.URL

	rep := c.GrowthRunTasks(testAuth(), GrowthTaskOptions{DryRun: true, Only: []string{"chat_5"}})
	for _, o := range rep.Tasks {
		if o.TaskCode != "chat_5" && o.TaskCode != "(批量接受)" {
			t.Fatalf("Only 过滤失效，出现了 %s", o.TaskCode)
		}
	}
}

// 需手动完成的任务应汇总进 Manual，供面板提示「这些要你自己在客户端做」。
func TestGrowthRunTasksReportsManualTasks(t *testing.T) {
	fastGaps(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(taskListJSON(
			map[string]any{"task_code": "Expert_Philanthropy", "title": "公益任务",
				"progress": map[string]any{"current": 0, "target": 1}},
			map[string]any{"task_code": "已锁定的任务", "locked": true,
				"progress": map[string]any{"current": 0, "target": 1}},
		)))
	}))
	defer srv.Close()

	c := New()
	c.ChatBaseCN, c.BillingBaseCN, c.WebBaseCN = srv.URL, srv.URL, srv.URL

	rep := c.GrowthRunTasks(testAuth(), GrowthTaskOptions{DryRun: true})
	if len(rep.Manual) != 1 || rep.Manual[0] != "Expert_Philanthropy" {
		t.Fatalf("Manual=%v want [Expert_Philanthropy]（锁定任务不应计入）", rep.Manual)
	}
	if !strings.Contains(rep.Msg, "需在客户端手动完成") {
		t.Fatalf("总结未提示手动项：%s", rep.Msg)
	}
}

// GrowthRunTasksOnce 是调度器入口：只返回基本类型，且把整体失败转成 error。
func TestGrowthRunTasksOnceSummary(t *testing.T) {
	fastGaps(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(taskListJSON(
			map[string]any{"task_code": "SomeManualTask", "title": "x",
				"progress": map[string]any{"current": 1, "target": 1}},
		)))
	}))
	defer srv.Close()

	c := New()
	c.ChatBaseCN, c.BillingBaseCN, c.WebBaseCN = srv.URL, srv.URL, srv.URL

	msg, credit, energy, err := c.GrowthRunTasksOnce(testAuth(), false)
	if err != nil {
		t.Fatalf("不应失败：%v", err)
	}
	if credit != 0 || energy != 0 {
		t.Fatalf("该任务无奖励字段，应为 0：%d/%d", credit, energy)
	}
	if msg == "" {
		t.Fatal("摘要不应为空（调度器要打日志）")
	}
	if !c.GrowthTasksSupported() {
		t.Fatal("workbuddy 客户端应报告支持成长任务")
	}
}

// stringsToErr 把文案转成 error，供复用 IsGrowthUnsupported。
func stringsToErr(s string) error { return &Error{Status: 404, Msg: s} }
