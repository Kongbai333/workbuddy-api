// 首次启动的账号导入：把旧项目（workbuddy-wild）的凭据复制到本项目目录。
//
// 为什么需要：新旧项目共用同一套数据布局（auths/ + config.json + data/），
// 但**目录不同**。若不导入，用户在新项目里要从零重新登录每个账号。
//
// 设计原则（保守）：
//   - 触发条件严格：真首次启动 + 目标 auths/ 为空 + 找到含凭据的源目录
//   - 只复制**凭据与配置**，不复制 data/ —— 冷却/签到状态是运行时产物，
//     带过来会引入过期冷却，反而制造问题
//   - 全程写日志，用户可从日志复原「复制了什么、从哪来」
//   - 任何一步失败都只记日志、不阻塞启动
//
// 可用 `--import-from <dir>` 显式指定源目录；未指定时按候选清单探测。
package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"workbuddy-api/internal/config"
)

// defaultImportSources 未显式指定 --import-from 时，按顺序探测的「旧项目目录」候选。
// 命中条件：该目录下存在非空的 auths/。
var defaultImportSources = []string{
	`D:\Program Files\workbuddy-wild`,                                 // 旧项目的历史部署目录
	filepath.Join(os.Getenv("USERPROFILE"), "wbsync", "build", "bin"), // 旧项目构建输出目录
}

// importFromArg 解析 `--import-from <dir>` / `--import-from=<dir>`。
func importFromArg(args []string) string {
	for i, a := range args {
		if a == "--import-from" && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "--import-from="); ok {
			return v
		}
	}
	return ""
}

// countJSONFiles 统计目录下 .json 文件数量（用于判断「有没有凭据」）。
func countJSONFiles(dir string) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			n++
		}
	}
	return n
}

// copyFile 复制单个文件（保留权限位）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// maybeFirstRunImport 首次启动时从旧项目目录导入账号凭据。
//
// 返回导入的账号数（0 表示未导入）。仅在以下条件**全部**满足时动作：
//  1. firstRun：本次启动时 config.json 不存在（真首次启动）
//  2. 目标 auths/ 下没有任何 .json（避免覆盖已有账号）
//  3. 找到了含凭据的源目录
func maybeFirstRunImport(cfg *config.Config, srcDirArg string, firstRun bool) int {
	if !firstRun {
		return 0
	}
	if n := countJSONFiles(cfg.AuthDir); n > 0 {
		log.Printf("首次导入跳过：目标账号目录已有 %d 个凭据", n)
		return 0
	}

	src := srcDirArg
	explicit := src != ""
	if !explicit {
		for _, cand := range defaultImportSources {
			if cand == "" {
				continue
			}
			if countJSONFiles(filepath.Join(cand, "auths")) > 0 {
				src = cand
				break
			}
		}
	}
	if src == "" {
		log.Printf("首次启动：未发现可导入的旧项目目录，从空配置开始")
		return 0
	}

	srcAuths := filepath.Join(src, "auths")
	if countJSONFiles(srcAuths) == 0 {
		if explicit {
			log.Printf("首次导入：指定的源目录 %s 下没有 auths/*.json，跳过", src)
		}
		return 0
	}

	log.Printf("首次启动：从 %s 导入账号凭据（来源%s）", src,
		map[bool]string{true: "：命令行指定", false: "：自动探测"}[explicit])

	if err := os.MkdirAll(cfg.AuthDir, 0o755); err != nil {
		log.Printf("首次导入失败：无法创建账号目录 %s：%v", cfg.AuthDir, err)
		return 0
	}

	ents, err := os.ReadDir(srcAuths)
	if err != nil {
		log.Printf("首次导入失败：读取源账号目录 %s：%v", srcAuths, err)
		return 0
	}
	imported, failed := 0, 0
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		// 备份文件（如 .bak-v057）不导入
		if strings.Contains(name, ".bak") {
			continue
		}
		dst := filepath.Join(cfg.AuthDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue // 已存在，不覆盖
		}
		if err := copyFile(filepath.Join(srcAuths, name), dst); err != nil {
			log.Printf("首次导入：%s 复制失败：%v", name, err)
			failed++
			continue
		}
		imported++
	}

	// 配置：仅当目标原本没有配置时，把源配置作为起点。
	// 用「不存在才复制」而非「覆盖」，避免覆盖用户刚在面板改过的设置。
	srcCfg := filepath.Join(src, "config.json")
	if _, err := os.Stat(srcCfg); err == nil {
		noteImportedConfig(src)
	}

	log.Printf("首次导入完成：账号 %d 个成功、%d 个失败（来源 %s）", imported, failed, src)
	log.Printf("注意：未导入 data/ 下的冷却与签到状态（属运行时产物，带过来会引入过期冷却）")
	return imported
}

// noteImportedConfig 记录源配置的位置，供用户参考。
//
// 这里**不自动复制 config.json**：新旧项目的配置项集合可能不同，
// 直接带过来可能引入不认识的字段或与新版默认值冲突。
// 更安全的做法是让用户在新面板里按需设置，因此只提示来源。
func noteImportedConfig(src string) {
	log.Printf("提示：检测到源配置 %s", filepath.Join(src, "config.json"))
	log.Printf("      本程序未自动复制配置（避免字段不兼容），请在管理面板中按需设置；")
	log.Printf("      若想沿用旧配置，可手工复制该文件到本项目目录后重启。")
}
