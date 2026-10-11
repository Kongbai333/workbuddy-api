package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEntrypointScriptProducesLoadableConfig 是「shell 壳 ↔ Go 结构体」的契约测试。
//
// 为什么需要它：`docker/entrypoint.sh` 在容器内用**字符串拼 JSON** 生成 config.json，
// 字段名是手写的；而 Go 侧靠 `json` tag 反序列化。两边一旦拼写不一致
// （如 `admin_pass` vs `admin_password`），Go 会**静默忽略**该字段并回退默认值 ——
// 现象是「Compose 里明明设了管理密码，面板却不要求登录」或
// 「设了 0.0.0.0 监听却仍在 127.0.0.1」，没有任何报错，极难定位。
//
// 本用例直接**执行真实的 entrypoint.sh** 生成配置，再用 Load 解析并逐字段断言，
// 从而把这条契约钉死。shell 不可用时跳过（CI 环境通常有）。
func TestEntrypointScriptProducesLoadableConfig(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("环境无 sh，跳过壳-Go 契约测试")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "docker", "entrypoint.sh")
	if _, err := os.Stat(script); err != nil {
		t.Skipf("找不到 docker/entrypoint.sh：%v", err)
	}

	dir := filepath.ToSlash(t.TempDir())
	authDir := dir + "/auths"
	stateFile := dir + "/state.json"

	cmd := exec.Command(sh, script)
	cmd.Env = append(os.Environ(),
		"WILDWORK_HOME="+dir,
		"WILDWORK_LISTEN=0.0.0.0:17863",
		"WILDWORK_ADMIN_PASSWORD=contract-admin-pass",
		"WILDWORK_API_KEY=contract-api-key",
		"WILDWORK_AUTH_DIR="+authDir,
		"WILDWORK_STATE_FILE="+stateFile,
		"WILDWORK_REGION=cn",
		"WILDWORK_TIMEOUT_SECONDS=90",
		"WILDWORK_ERR_THRESHOLD=5",
		"WILDWORK_GROWTH_REDEEM_STREAK=on",
		"WILDWORK_GROWTH_TRAVEL=1",
		"WILDWORK_GROWTH_TRAVEL_LOCATION=3",
	)
	// entrypoint 末尾会 exec 容器内的二进制；本测试环境没有它，退出必然非 0。
	// 这不是失败 —— 我们只关心「exec 之前 config.json 是否已正确写盘」。
	_ = cmd.Run()

	c, err := Load(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("entrypoint 生成的配置无法被 Go 解析（字段名可能不一致）：%v", err)
	}

	if got := c.Listen.Addr(); got != "0.0.0.0:17863" {
		t.Errorf("listen 未打通：got %q（期望 0.0.0.0:17863）", got)
	}
	if c.AdminPass != "contract-admin-pass" {
		t.Errorf("admin_password 未打通：got %q —— 面板将不会要求登录", c.AdminPass)
	}
	if c.APIKey != "contract-api-key" {
		t.Errorf("api_key 未打通：got %q —— 接口将无鉴权", c.APIKey)
	}
	if c.AuthDir != authDir {
		t.Errorf("auth_dir 未打通：got %q", c.AuthDir)
	}
	if c.StateFile != stateFile {
		t.Errorf("state_file 未打通：got %q", c.StateFile)
	}
	if c.Region != "cn" {
		t.Errorf("region 未打通：got %q", c.Region)
	}
	if c.Upstream.TimeoutSeconds != 90 {
		t.Errorf("timeout_seconds 未打通：got %d", c.Upstream.TimeoutSeconds)
	}
	if c.Cooldown.ErrThresh != 5 {
		t.Errorf("err_threshold 未打通：got %d", c.Cooldown.ErrThresh)
	}
	if !c.Schedule.GrowthRedeemStreak {
		t.Error("growth_redeem_streak 未打通（传的 on 应被识别为 true）")
	}
	if !c.Schedule.GrowthTravel {
		t.Error("growth_travel 未打通（传的 1 应被识别为 true）")
	}
	if c.Schedule.GrowthTravelLocation != 3 {
		t.Errorf("growth_travel_location 未打通：got %d", c.Schedule.GrowthTravelLocation)
	}
	// 未在 entrypoint 里显式写出的字段，必须落到默认值（而不是零值），
	// 否则 normalize 会在 time.ParseDuration("") 上直接失败。
	if c.Cooldown.HardCredit != "12h" || c.Cooldown.SoftRate != "60s" {
		t.Errorf("默认值丢失：hard=%q soft=%q", c.Cooldown.HardCredit, c.Cooldown.SoftRate)
	}
	if c.HardCreditDur == 0 {
		t.Error("hard_credit 的解析结果未生效（normalize 未跑）")
	}
}

// TestEntrypointRefusesRemoteListenWithoutPassword 锁定 entrypoint 的安全护栏：
// 「对外监听 + 未设管理密码」必须**拒绝启动**（退出码 1），
// 与 Go 侧启动检查（main.go 的 fatal）保持一致的语义 —— 双保险，
// 避免用户在容器里绕过 Go 的检查（例如未来 Go 侧放宽约束时）。
func TestEntrypointRefusesRemoteListenWithoutPassword(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("环境无 sh，跳过")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "docker", "entrypoint.sh")
	if _, err := os.Stat(script); err != nil {
		t.Skipf("找不到 docker/entrypoint.sh：%v", err)
	}

	cases := []struct {
		name       string
		listen     string
		pass       string
		wantReject bool
	}{
		{"对外监听+无密码", ":7863", "", true},
		{"0.0.0.0+无密码", "0.0.0.0:7863", "", true},
		{"对外监听+有密码", ":7863", "some-strong-pass", false},
		{"环回+无密码", "127.0.0.1:7863", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 并行：每次都要冷启动一个 sh 进程（Git Bash 下约 7s），
			// 串行跑 4 个变体会让这个包多出 30 秒 —— 对全量测试太贵。
			// 各子测试用独立的临时目录与显式 cmd.Env，互不干扰。
			t.Parallel()
			dir := filepath.ToSlash(t.TempDir())
			cmd := exec.Command(sh, script)
			cmd.Env = append(os.Environ(),
				"WILDWORK_HOME="+dir,
				"WILDWORK_LISTEN="+tc.listen,
				"WILDWORK_ADMIN_PASSWORD="+tc.pass,
				"WILDWORK_AUTH_DIR="+dir+"/auths",
				"WILDWORK_STATE_FILE="+dir+"/state.json",
			)
			_ = cmd.Run()

			// 取真实退出码：护栏分支是 exit 1；放行分支则因容器内二进制不存在
			// 而以 127 结束（exec 失败），二者可区分。
			code := 0
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}
			if rejected := code == 1; rejected != tc.wantReject {
				t.Errorf("退出码=%d（拒绝启动=%v），期望拒绝启动=%v", code, rejected, tc.wantReject)
			}
		})
	}
}
