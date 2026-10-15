package config

// login_callback_test.go 锁定容器部署的登录回调基址解析。
//
// 这条配置是「TraeWork / 千问办公 在容器里登录永远卡在等待」的修复开关，
// 两个约束必须钉死：
//   - 留空 = 老行为（裸机零回归）；
//   - 给了值就必须能算出「浏览器够得着的地址」与「该绑哪个网卡」，
//     且非法值在启动时就报错 —— 登录失败发生在用户浏览器里，看不到日志。

import (
	"path/filepath"
	"testing"
)

func TestParseLoginCallback(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantListen string
		wantBase   string
		wantErr    bool
	}{
		{name: "留空=老行为", raw: "", wantListen: "", wantBase: ""},
		{name: "空白=老行为", raw: "   ", wantListen: "", wantBase: ""},
		{
			name: "回环+端口（浏览器与 Docker 宿主机同机）",
			raw:  "http://127.0.0.1:17863",
			// 监听必须落在全部网卡：容器里 127.0.0.1 只有容器自己够得着
			wantListen: "0.0.0.0:17863", wantBase: "http://127.0.0.1:17863",
		},
		{
			name: "局域网 IP（浏览器在别的机器）",
			raw:  "http://192.168.1.10:17863",
			// 监听仍是 0.0.0.0：192.168.1.10 是宿主机的地址，容器绑不上
			wantListen: "0.0.0.0:17863", wantBase: "http://192.168.1.10:17863",
		},
		{
			name: "省略 scheme", raw: "127.0.0.1:17863",
			wantListen: "0.0.0.0:17863", wantBase: "http://127.0.0.1:17863",
		},
		{
			name: "省略端口取默认", raw: "127.0.0.1",
			wantListen: "0.0.0.0:17863", wantBase: "http://127.0.0.1:17863",
		},
		{
			name: "尾斜杠规范化", raw: "http://127.0.0.1:17863/",
			wantListen: "0.0.0.0:17863", wantBase: "http://127.0.0.1:17863",
		},
		{name: "拒绝 https（监听是明文 http，写了会静默连不上）", raw: "https://nas.local:17863", wantErr: true},
		{name: "拒绝带路径", raw: "http://127.0.0.1:17863/cb", wantErr: true},
		{name: "拒绝非法端口", raw: "http://127.0.0.1:99999", wantErr: true},
		{name: "拒绝缺主机名", raw: "http://:17863", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			listen, base, err := ParseLoginCallback(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，got listen=%q base=%q", listen, base)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误：%v", err)
			}
			if listen != tc.wantListen || base != tc.wantBase {
				t.Fatalf("got (%q, %q)，期望 (%q, %q)", listen, base, tc.wantListen, tc.wantBase)
			}
		})
	}
}

// TestNormalizeRejectsBadLoginCallback 非法值必须在启动（normalize）时报错，
// 而不是等到用户点「添加账号」才在浏览器里失败。
func TestNormalizeRejectsBadLoginCallback(t *testing.T) {
	c := Default()
	c.LoginCallback = "https://127.0.0.1:17863"
	if err := c.normalize(); err == nil {
		t.Fatal("非法 login_callback 应让 normalize 报错")
	}
}

// TestNormalizeCanonicalizesLoginCallback 合法值被规范化后回写配置，
// 便于用户从 config.json 一眼看出实际生效的地址。
func TestNormalizeCanonicalizesLoginCallback(t *testing.T) {
	c := Default()
	c.LoginCallback = "127.0.0.1/"
	if err := c.normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if c.LoginCallback != "http://127.0.0.1:17863" {
		t.Fatalf("未规范化：got %q", c.LoginCallback)
	}
}

// TestEnvOverridesLoginCallback 环境变量（容器部署的唯一入口）必须能覆盖配置。
func TestEnvOverridesLoginCallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := Default()
	if err := Save(c, path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WILDWORK_LOGIN_CALLBACK", "192.168.1.10:18000")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LoginCallback != "http://192.168.1.10:18000" {
		t.Fatalf("env 未打通：got %q", got.LoginCallback)
	}
}
