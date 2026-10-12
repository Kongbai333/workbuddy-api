package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Listen.Addr() != "127.0.0.1:7863" {
		t.Errorf("listen=%s", c.Listen.Addr())
	}
	// APIKey 现在是随机生成的：只断言「非空」与「长度合理」。
	//
	// 不能断言具体值 —— 旧测试锁死了一个固定常量，而那个值恰好与开发者
	// 实际使用中的 key 相同，会随二进制和公开仓库一起泄露。
	// 随机化后这类断言天然失效，正是要的效果。
	if c.APIKey == "" {
		t.Error("api_key 为空：默认配置必须自带一个可用的 key")
	}
	if len(c.APIKey) < 20 {
		t.Errorf("api_key 过短（%d 字符），熵不足", len(c.APIKey))
	}
	if err := c.normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if c.HardCreditDur.Hours() != 12 {
		t.Errorf("hard=%v", c.HardCreditDur)
	}
}

// TestDefaultAPIKeyRandom 锁定「每个实例的默认 key 不同」——
// 这是随机化的全部意义所在（固定默认值会随二进制公开）。
func TestDefaultAPIKeyRandom(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		k := Default().APIKey
		if seen[k] {
			t.Fatalf("第 %d 次生成的 key 与之前重复：%q（随机源有问题）", i, k)
		}
		seen[k] = true
	}
}

func TestListenParsing(t *testing.T) {
	cases := []struct {
		in   string
		addr string
	}{
		{":9999", ":9999"},
		{"127.0.0.1:9999", "127.0.0.1:9999"},
		{"9999", ":9999"},
		{"0.0.0.0:7863", "0.0.0.0:7863"},
	}
	for _, c := range cases {
		l, err := ParseListen(c.in)
		if err != nil {
			t.Errorf("ParseListen(%q): %v", c.in, err)
			continue
		}
		if got := l.Addr(); got != c.addr {
			t.Errorf("ParseListen(%q).Addr()=%s want %s", c.in, got, c.addr)
		}
	}
	if _, err := ParseListen(":notaport"); err == nil {
		t.Error("want error for bad port")
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "c.json")
	os.WriteFile(fp, []byte(`{"listen":"127.0.0.1:9999","api_key":"k","region":"cn"}`), 0o600)
	c, err := Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen.Addr() != "127.0.0.1:9999" || c.APIKey != "k" {
		t.Errorf("c=%+v", c)
	}
}

func TestLoadFileObjectListen(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "c.json")
	os.WriteFile(fp, []byte(`{"listen":{"host":"192.168.1.2","port":8000}}`), 0o600)
	c, err := Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen.Addr() != "192.168.1.2:8000" {
		t.Errorf("addr=%s", c.Listen.Addr())
	}
}

func TestSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "sub", "c.json")
	c := Default()
	c.Listen.Host = "127.0.0.1"
	c.Schedule.CheckinHours = []int{8, 20}
	if err := Save(c, fp); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Listen.Addr() != "127.0.0.1:7863" {
		t.Errorf("addr=%s", c2.Listen.Addr())
	}
	if len(c2.Schedule.CheckinHours) != 2 || c2.Schedule.CheckinHours[0] != 8 {
		t.Errorf("hours=%v", c2.Schedule.CheckinHours)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("WILDWORK_LISTEN", ":7777")
	t.Setenv("WILDWORK_API_KEY", "envkey")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen.Addr() != ":7777" || c.APIKey != "envkey" {
		t.Errorf("c=%+v", c)
	}
}

func TestClockTimes(t *testing.T) {
	got, err := ParseClockTimes([]string{"21:30", "09:05", "09:05"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 9*60+5 || got[1] != 21*60+30 {
		t.Fatalf("minutes=%v", got)
	}
	if _, err := ParseClockTimes([]string{"24:00"}); err == nil {
		t.Fatal("want invalid time error")
	}
	if got := FormatClockTimes(got); got[0] != "09:05" || got[1] != "21:30" {
		t.Fatalf("times=%v", got)
	}
}

func TestLoadLegacyCheckinHours(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "c.json")
	os.WriteFile(fp, []byte(`{"schedule":{"checkin_hours":[8,20]}}`), 0o600)
	c, err := Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Schedule.CheckinTimes; len(got) != 2 || got[0] != "08:00" || got[1] != "20:00" {
		t.Fatalf("times=%v", got)
	}
}

func TestLoadMinuteCheckinTimes(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "c.json")
	os.WriteFile(fp, []byte(`{"schedule":{"checkin_times":["09:05","21:30"]}}`), 0o600)
	c, err := Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Schedule.CheckinTimes; len(got) != 2 || got[0] != "09:05" || got[1] != "21:30" {
		t.Fatalf("times=%v", got)
	}
}

func TestAdminPasswordAndLoopback(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "c.json")
	// 旧配置（无 admin_password）必须能加载，且默认空 = 面板不鉴权
	os.WriteFile(fp, []byte(`{"listen":"127.0.0.1:9999"}`), 0o600)
	c, err := Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if c.AdminPass != "" {
		t.Errorf("缺省 admin_password 应为空，实际 %q", c.AdminPass)
	}
	// 新字段可读
	os.WriteFile(fp, []byte(`{"listen":{"host":"0.0.0.0","port":8000},"admin_password":"pw12345678"}`), 0o600)
	if c, err = Load(fp); err != nil {
		t.Fatal(err)
	} else if c.AdminPass != "pw12345678" {
		t.Errorf("admin_password=%q", c.AdminPass)
	}
	// env 覆盖
	t.Setenv("WILDWORK_ADMIN_PASSWORD", "env-pw")
	if c, err = Load(fp); err != nil || c.AdminPass != "env-pw" {
		t.Errorf("env override 失败：%v %q", err, c.AdminPass)
	}
	// IsLoopback
	for host, want := range map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true, "0.0.0.0": false, "": false, "192.168.1.9": false} {
		if got := (Listen{Host: host}).IsLoopback(); got != want {
			t.Errorf("IsLoopback(%q)=%v want %v", host, got, want)
		}
	}
}

func TestBadDuration(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "c.json")
	os.WriteFile(fp, []byte(`{"cooldown":{"hard_credit":"not-a-duration"}}`), 0o600)
	if _, err := Load(fp); err == nil {
		t.Fatal("want error for bad duration")
	}
}

// TestStrategyDefaultAndNormalize 默认策略是 expire（临期优先），
// 空串/大小写/空白都能归一，非法值必须报错而不是静默回落。
//
// 为什么非法值报错：静默回落会让用户以为切到了 roundrobin，
// 实际却在跑 expire —— 表现为「设置没生效」且极难自查。
func TestStrategyDefaultAndNormalize(t *testing.T) {
	if got := Default().Strategy; got != "expire" {
		t.Fatalf("默认策略=%q，应为 expire", got)
	}
	for _, in := range []string{"credits", "CREDITS", " roundrobin ", "Expire"} {
		c := Default()
		c.Strategy = in
		if err := c.normalize(); err != nil {
			t.Fatalf("normalize(%q): %v", in, err)
		}
		want := map[string]string{"credits": "credits", "CREDITS": "credits",
			" roundrobin ": "roundrobin", "Expire": "expire"}[in]
		if c.Strategy != want {
			t.Errorf("normalize(%q)=%q want %q", in, c.Strategy, want)
		}
	}
	// 空串 → 默认
	c := Default()
	c.Strategy = ""
	if err := c.normalize(); err != nil {
		t.Fatal(err)
	}
	if c.Strategy != "expire" {
		t.Errorf("空串策略=%q，应归一为 expire", c.Strategy)
	}
	// 非法值报错
	c = Default()
	c.Strategy = "balance"
	if err := c.normalize(); err == nil {
		t.Error("非法 strategy 应报错而非静默回落")
	}
}

// TestStrategyEnvOverride 环境变量可覆盖 strategy（容器部署走这条路）。
func TestStrategyEnvOverride(t *testing.T) {
	t.Setenv("WILDWORK_STRATEGY", "roundrobin")
	c := Default()
	applyEnv(c)
	if err := c.normalize(); err != nil {
		t.Fatal(err)
	}
	if c.Strategy != "roundrobin" {
		t.Fatalf("env 未生效: %q", c.Strategy)
	}
}

// TestLoadMissingAPIKeyKeepsDefault 锁定一个反直觉但重要的行为：
// **配置里缺少 api_key 字段 ≠ 关闭鉴权**。
//
// Load 的流程是先 Default()（填随机 key）再用 JSON 覆盖，因此：
//   - 缺 api_key 字段        → 保留 Default() 的随机值 → 仍有鉴权
//   - 显式 "api_key": ""     → 覆盖成空 → VerifyBearer 首行放行 → **无鉴权**
//
// 写这个测试是因为我一度在启动提示里断言「删掉字段会退回空值」，
// 实测发现是错的。凭印象写错误提示比不写更糟 —— 用户会照着错误理解去排查。
func TestLoadMissingAPIKeyKeepsDefault(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(fp, []byte(`{"listen":{"host":"127.0.0.1","port":7863}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(fp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.APIKey == "" {
		t.Fatal("缺少 api_key 字段时应保留随机默认值，而不是变成空（空 = 无鉴权）")
	}
}

// TestLoadExplicitEmptyAPIKeyDisablesAuth 与上一个测试成对：
// 显式空字符串**确实**会关闭鉴权，这是 main.go 里拒绝启动的那条路径。
func TestLoadExplicitEmptyAPIKeyDisablesAuth(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(fp, []byte(`{"api_key":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(fp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.APIKey != "" {
		t.Fatalf("显式空字符串应关闭鉴权，实际=%q", c.APIKey)
	}
}
