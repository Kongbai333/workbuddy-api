//go:build !windows && !darwin

package platform

import (
	"os"
	"os/exec"
)

// WorkArea 其他平台：返回 0,0,0,0（未实现）。
func WorkArea() (int32, int32, int32, int32) { return 0, 0, 0, 0 }

// InfoBox 其他平台：输出到 stderr。
func InfoBox(title, msg string) { Notify(title, msg) }

// Notify 其他平台：输出到 stderr。
func Notify(title, msg string) {
	_, _ = os.Stderr.WriteString(title + ": " + msg + "\n")
}

// AskYesNo 其他平台：默认否。
func AskYesNo(title, msg string) bool { return false }

// DefaultBrowserIncognito 其他平台：不探测，走系统默认。
func DefaultBrowserIncognito() (exePath, flag string, ok bool) { return "", "", false }

// LaunchIncognito 其他平台：直接 open url。
func LaunchIncognito(exe, flag, url string) error { return OpenURL(url) }

// OpenURL 其他平台：xdg-open。
func OpenURL(url string) error { return exec.Command("xdg-open", url).Start() }

// SetAutostart 其他平台：未实现。
func SetAutostart(on bool) error { return nil }

// AutostartEnabled 其他平台：false。
func AutostartEnabled() bool { return false }

// OpenWithTextEditor 其他平台：xdg-open。
func OpenWithTextEditor(path string) error { return exec.Command("xdg-open", path).Start() }

// AcquireSingleInstance 其他平台：当前为无操作（恒返回 true）。
// 理由同 platform_darwin.go —— 真实实现只在 Windows。
// Linux 下通常以 --no-tray 无头运行，重复启动会因端口被占而立即失败。
func AcquireSingleInstance(name string) bool { return true }
