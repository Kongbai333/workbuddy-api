package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrCreateAppliesEnvOnFirstRun 是本文件的核心用例：
// 配置文件不存在（容器首次启动的典型情形）时，WILDWORK_* 环境变量**必须**生效。
//
// 回归背景：早期启动路径在「配置文件不存在」分支直接取 Default()，跳过了 applyEnv，
// 导致纯 env 部署静默失效 —— 容器里表现为「进程起来了，但 7863 端口不通」，
// 且日志无任何异常，极难定位。
func TestLoadOrCreateAppliesEnvOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	t.Setenv("WILDWORK_LISTEN", ":17863")
	t.Setenv("WILDWORK_API_KEY", "env-key-123")
	t.Setenv("WILDWORK_ADMIN_PASSWORD", "env-admin-pass")
	t.Setenv("WILDWORK_AUTH_DIR", "/data/auths")

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("前置失败：文件本不应存在")
	}

	c, created, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if !created {
		t.Error("首次创建应返回 created=true")
	}
	if got := c.Listen.Addr(); got != ":17863" {
		t.Errorf("env 的 listen 未生效：got %q", got)
	}
	if c.APIKey != "env-key-123" {
		t.Errorf("env 的 api_key 未生效：got %q", c.APIKey)
	}
	if c.AdminPass != "env-admin-pass" {
		t.Errorf("env 的 admin_password 未生效：got %q", c.AdminPass)
	}
	if c.AuthDir != "/data/auths" {
		t.Errorf("env 的 auth_dir 未生效：got %q", c.AuthDir)
	}
	// 未由 env 提供的字段应保留默认值，而不是被清零 ——
	// 若被清零，normalize 会在 time.ParseDuration("") 上直接报错。
	if c.Upstream.TimeoutSeconds != 120 {
		t.Errorf("默认值丢失：timeout_seconds=%d", c.Upstream.TimeoutSeconds)
	}
	if c.Cooldown.HardCredit != "12h" {
		t.Errorf("默认值丢失：hard_credit=%q", c.Cooldown.HardCredit)
	}
	if c.Cooldown.ErrThresh != 3 {
		t.Errorf("默认值丢失：err_threshold=%d", c.Cooldown.ErrThresh)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("生成的配置未落盘：%v", err)
	}
}

// TestLoadOrCreateIsIdempotent 生成的文件必须能被再次加载，且两次结果一致。
// （若首次生成的 JSON 不合法或字段名写错，第二次加载会直接失败 ——
// 这是「生成的配置能不能被自己读懂」的自检。）
func TestLoadOrCreateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("WILDWORK_LISTEN", ":17863")

	c1, created, err := LoadOrCreate(path)
	if err != nil || !created {
		t.Fatalf("首次创建失败：err=%v created=%v", err, created)
	}
	c2, created2, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("二次加载: %v", err)
	}
	if created2 {
		t.Error("二次加载不应再报 created=true")
	}
	if c2.Listen.Addr() != c1.Listen.Addr() || c2.APIKey != c1.APIKey {
		t.Errorf("二次加载与首次不一致：listen %q vs %q", c2.Listen.Addr(), c1.Listen.Addr())
	}
	if c2.Cooldown.HardCredit != "12h" || c2.Cooldown.SoftRate != "60s" {
		t.Errorf("二次加载丢失默认值：hard=%q soft=%q", c2.Cooldown.HardCredit, c2.Cooldown.SoftRate)
	}
	if c2.Schedule.KeepaliveHours == nil {
		t.Error("二次加载丢失 keepalive_hours（日程默认值）")
	}
}

// TestLoadOrCreateInvalidFileReturnsError 非法配置必须报错，不能被当成「首次创建」悄悄覆盖 ——
// 否则用户手改坏一个字母，程序会把他的配置直接抹掉重写。
func TestLoadOrCreateInvalidFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, created, err := LoadOrCreate(path)
	if err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
	if created {
		t.Error("非法文件不应被当作首次创建")
	}
	// 原文件必须原样保留，不能被覆盖。
	raw, rerr := os.ReadFile(path)
	if rerr != nil || string(raw) != "{not json" {
		t.Errorf("非法配置被改写了：%q err=%v", raw, rerr)
	}
}

// TestLoadOrCreateDefaultsWithoutEnv 显式清空 env，确认拿到纯默认值。
// 用途：防止宿主机残留的 WILDWORK_* 让本用例与上一条互相污染。
func TestLoadOrCreateDefaultsWithoutEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	for _, k := range []string{
		"WILDWORK_LISTEN", "WILDWORK_API_KEY", "WILDWORK_ADMIN_PASSWORD",
		"WILDWORK_AUTH_DIR", "WILDWORK_STATE_FILE", "WILDWORK_REGION",
	} {
		t.Setenv(k, "")
	}
	c, created, err := LoadOrCreate(path)
	if err != nil || !created {
		t.Fatalf("err=%v created=%v", err, created)
	}
	if got := c.Listen.Addr(); got != "127.0.0.1:7863" {
		t.Errorf("无 env 时应为默认监听：got %q", got)
	}
	if c.Region != "cn" {
		t.Errorf("无 env 时应为默认区域 cn：got %q", c.Region)
	}
}
