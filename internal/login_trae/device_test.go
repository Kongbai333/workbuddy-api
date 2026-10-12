package login_trae

// device_test.go 客户端 storage.json 设备号提取的回归测试。
//
// 为什么必须来自客户端：claim 按「注册指纹」校验 device id，自造号不被
// 认作注册设备 → 恒 9074（见 device.go 顶部实测依据）。

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExtractDeviceIDFromKeyName 设备号写在**键名**上（iCubeAuthInfo://icube-dc:NNNN），
// 必须能从键名提取，而不是从值。
func TestExtractDeviceIDFromKeyName(t *testing.T) {
	content := `{
  "iCubeAuthInfo://icube-dc:4484256452647802": {"token":"t","uid":"u1"},
  "telemetry.devDeviceId": "9f2b1c3d-4e5a-6b7c-8d9e-0f1a2b3c4d5e",
  "other.key": {"x":1}
}`
	got := extractDeviceID([]byte(content))
	if got != "4484256452647802" {
		t.Fatalf("extractDeviceID=%q want 4484256452647802", got)
	}
}

// TestExtractDeviceIDFallbackValue 某些版本可能把设备号放在 deviceId 字段值里。
func TestExtractDeviceIDFallbackValue(t *testing.T) {
	got := extractDeviceID([]byte(`{"deviceId":"4484256452647802"}`))
	if got != "4484256452647802" {
		t.Fatalf("value 兜底失败: %q", got)
	}
}

// TestExtractDeviceIDNotFound 无关内容必须返回空串（调用方据此回退随机号），
// 且不能误取 UUID 形式的 devDeviceId。
func TestExtractDeviceIDNotFound(t *testing.T) {
	for _, content := range []string{
		`{}`,
		`{"telemetry.devDeviceId":"9f2b1c3d-4e5a-6b7c-8d9e-0f1a2b3c4d5e"}`,
		`{"iCubeAuthInfo://icube-dc:abc": {}}`, // 非数字，不认
		`not json at all`,
	} {
		if got := extractDeviceID([]byte(content)); got != "" {
			t.Fatalf("content=%q 应提取为空，实际 %q", content, got)
		}
	}
}

// TestExtractDeviceIDFromTempDir 在临时目录里放一个合法 storage.json 时能读到设备号。
// 直接测 extractDeviceID 与目录发现分开：目录发现依赖真实 APPDATA，
// 这里只验证「有内容 → 有设备号」这条解析路径。
func TestExtractDeviceIDFromTempDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "storage.json")
	content := `{"iCubeAuthInfo://icube-dc:4484256452647802":{"token":"t"}}`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := extractDeviceID(data); got != "4484256452647802" {
		t.Fatalf("got %q", got)
	}
}
