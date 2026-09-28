package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"agent_gateway/internal/config"
)

// Kind 是 agent 种类。
const (
	KindClaude = "claude"
	KindCodex  = "codex"
	// KindCodexDesktop 是 Linux Codex/ChatGPT 桌面应用入口。
	KindCodexDesktop = "codex-desktop"
)

// PrepareExec 解析项目与令牌，组装 exec 所需的 env/dir/argv。
// project 为空时按 cwd 推断（须位于 projects/ 下，否则用全局档案）。
func PrepareExec(root, kind, project string, extraArgs []string) (map[string]string, string, []string, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, "", nil, err
	}
	if project == "" {
		if inferred := inferProjectFromCwd(root); inferred != "" {
			project = inferred
		}
	}
	dir := filepath.Join(root, "projects", project)
	token := cfg.Gateway.DefaultToken
	if project != "" {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			return nil, "", nil, fmt.Errorf("项目不存在: %s（可用项目: %s）", project, listProjects(root))
		}
		if p, ok := cfg.Projects[project]; ok && p.Token != "" {
			token = p.Token
		}
	}
	listen := cfg.Gateway.Listen
	if listen == "" {
		listen = config.DefaultListen
	}
	env := map[string]string{}
	var argv []string
	switch kind {
	case KindClaude:
		// 独立 settings 文件承载网关地址与项目令牌（叠加层，用户 settings 继续生效）
		settingsPath, err := GenerateClaudeSettings(root, project, listen, token)
		if err != nil {
			return nil, "", nil, fmt.Errorf("生成 claude 独立配置失败: %w", err)
		}
		argv = append([]string{"claude", "--settings", settingsPath}, extraArgs...)
	case KindCodex:
		home, _ := os.UserHomeDir()
		if err := EnsureCodexProfile(CodexHome(home), listen); err != nil {
			return nil, "", nil, fmt.Errorf("生成 codex profile 失败: %w", err)
		}
		env["AGW_API_KEY"] = token
		argv = append([]string{"codex", "-p", "agw"}, extraArgs...)
	case KindCodexDesktop:
		app, err := resolveCodexDesktopApp()
		if err != nil {
			return nil, "", nil, err
		}
		managedHome, err := EnsureDesktopHome(root, listen, token)
		if err != nil {
			return nil, "", nil, fmt.Errorf("准备 Codex 桌面独立配置失败: %w", err)
		}
		env["CODEX_HOME"] = managedHome
		env["AGW_API_KEY"] = token
		argv = append([]string{app}, extraArgs...)
	default:
		return nil, "", nil, fmt.Errorf("未知 agent: %s（claude | codex | codex-desktop）", kind)
	}
	return env, dir, argv, nil
}

// inferProjectFromCwd 从 cwd 推断项目名：cwd 必须直接位于 <root>/projects/<名> 之下。
func inferProjectFromCwd(root string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	projectsDir := filepath.Join(root, "projects") + string(filepath.Separator)
	if strings.HasPrefix(cwd, projectsDir) {
		rest := strings.TrimPrefix(cwd, projectsDir)
		name := strings.SplitN(rest, string(filepath.Separator), 2)[0]
		if name != "" {
			return name
		}
	}
	return ""
}

// listProjects 列出 projects/ 下的项目名（逗号分隔）。
func listProjects(root string) string {
	entries, err := os.ReadDir(filepath.Join(root, "projects"))
	if err != nil || len(entries) == 0 {
		return "（无）"
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Exec 替换当前进程启动 agent（信号直达、无中间进程）。
func Exec(env map[string]string, dir string, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("空命令")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("未找到 %s（先 agw install %s）: %w", argv[0], argv[0], err)
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("进入项目目录 %s 失败: %w", dir, err)
	}
	final := appendOverrideEnv(os.Environ(), env)
	return syscall.Exec(path, argv, final)
}

// appendOverrideEnv 把覆盖键排序后追加到基础环境尾部；exec 后的运行时取最后值。
func appendOverrideEnv(base []string, overrides map[string]string) []string {
	final := make([]string, 0, len(base)+len(overrides))
	final = append(final, base...)
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		final = append(final, key+"="+overrides[key])
	}
	return final
}

// resolveCodexDesktopApp 从用户显式路径或 Linux 受限默认目录解析桌面应用。
func resolveCodexDesktopApp() (string, error) {
	home, _ := os.UserHomeDir()
	return ResolveCodexDesktopApp(DesktopResolveOptions{
		GOOS:     runtime.GOOS,
		Home:     home,
		Explicit: os.Getenv("AGW_CODEX_APP"),
	})
}

// PrepareResetDesktop 仅重置 agw 管理的 Codex 桌面独立 home。
func PrepareResetDesktop(root string) error {
	return ResetDesktopHome(root)
}
