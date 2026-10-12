// Package traework 封装 Trae SOLO 免费通道上游协议。
package traework

import "time"

const (
	AgentHost      = "https://trae-api-cn.mchost.guru"
	UgHost         = "https://api.trae.cn"
	OAuthHost      = "https://api.trae.com.cn"
	ConsoleHost    = "https://www.trae.cn"
	WorkHost       = "https://work.trae.cn" // Web 应用端，模型定价接口用
	ClientID       = "en1oxy7wnw8j9n"
	AppID          = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"
	IdeVersion     = "0.1.52" // 对齐 connectedGraph/traework2api 上游，对应更新更全的模型配置表（含 glm-5.3）
	IdeVersionCode = "20260811"
	DeviceBrand    = "20Y5A002XX"     // 设备机型示例（指纹头用，可替换为真实机型，无需精确匹配）
	OSVersion      = "Windows 10 Pro" // 真实客户端系统版本
	PluginVersion  = "2.3.73734"      // 真实客户端插件版本（登录 URL 用）
	Function       = "solo_work_lite" // 办公版（TraeWork）function
	FunctionCode   = "solo_agent"     // 代码版（TraeCode）function

	// 定价分组：Trae 同一上游有两个 function，各渠道记录自己所用的口径。
	// PricingFunctionsCode 必须含**无** _remote 后缀的 solo_agent，否则拿不到新版
	// flash 模型的费率（实测 deepseek-v4.1-flash、glm-5.3-flash 仅在 solo_agent
	// 分组下发）；_remote 分组用于补齐旧命名模型。
	// PricingPrimary* 是各渠道「主 function」：同一模型可能在多个分组下倍率不同
	// （实测豆包 Seed-2.1-Pro：solo_agent=0.08 / _remote=0.8），对话按主 function 计费。
	PricingFunctionsWork = "solo_agent_remote,solo_work_remote,solo_design_remote"
	PricingFunctionsCode = "solo_agent,solo_agent_remote,solo_work_remote,solo_design_remote"
	PricingPrimaryWork   = "solo_work_remote"
	PricingPrimaryCode   = "solo_agent"

	EpChat          = "/api/agent/v3/llm_utils_chat"
	EpModels        = "/api/ide/v1/get_detail_param"
	EpExchange      = "/cloudide/api/v3/trae/oauth/ExchangeToken"
	EpUserInfo      = "/cloudide/api/v3/trae/GetUserInfo"
	EpCheckinStatus = "/trae/api/v2/ug/checkin_credits/status"
	EpCheckinClaim  = "/trae/api/v2/ug/checkin_credits/claim"
	EpEntUsage      = "/trae/api/v2/pay/web_user_ent_usage" // 网页版积分接口（带 require_usage 拿实际用量）
	EpModelsPricing = "/api/remote/v1/models"               // 模型定价接口
)

// 签到业务码。
//
// ⚠️ 这两个码的定性经历了两次反转，最终结论来自 2026-10-01~10-02 实测，
// 详见 client.go 的 CheckinClaim 与 claimWithRotatedDevice 注释。
const (
	// CheckinRateLimitCode 9074「当前参与用户太多，请稍后再试」。
	//
	// **是瞬时限流，不是「设备号未注册」**（v0.5.7~v0.6.8 曾误判为后者）。
	// 实测同一操作有时成功、有时 9074，稍后重试或换号即可通过。
	CheckinRateLimitCode = 9074

	// CheckinAlreadyClaimedCode 9095「当前设备今日已经签到，请明日再来哦～」。
	//
	// ⚠️ 关键：它是**设备维度**的拒绝，而额度发放是**账号维度**的。
	// 同一设备号下只要有一个账号领过，其他当天没领的账号就会被连带拒绝 ——
	// 换一个全新设备号即可正常入账（实测 4600 → 4700）。
	//
	// 这正是 2026-10-12 事故的成因：两个 TraeWork 账号共用设备号 44842564…，
	// 先签的那个用掉当天额度，后签的连撞两次 9095、100 积分没领到。
	CheckinAlreadyClaimedCode = 9095
)

// maxRotateAttempts 换设备号重试的最大尝试次数。
//
// 为什么是多次而非一次：换号请求本身有一定概率撞上 9074（瞬时限流），
// 只试一次正好撞上就彻底放弃，会让本可成功的账号当天签不到。
const maxRotateAttempts = 8

const DefaultConfigName = "glm-5.2"

// softRateResetLoc 上游时间戳展示口径：固定按 UTC+8 墙钟解释。
// 上游下发的 Unix 秒与官网展示的日期均以国内时区为准，用本地时区格式化会在
// 非 UTC+8 机器上把到期日算错一天。
var softRateResetLoc = time.FixedZone("UTC+8", 8*60*60)
