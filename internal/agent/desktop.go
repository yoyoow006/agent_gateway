package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Linux 桌面应用自动发现只使用这些一级 roots 与名称，不做全盘扫描。
var linuxDesktopAppDirs = []string{"ChatGPT", "chatgpt", "Codex", "codex", "codex-beta"}
var linuxDesktopExecutables = []string{"ChatGPT", "chatgpt", "Codex", "codex"}

// DesktopResolveOptions 注入平台、home、roots 与谓词，保证测试不依赖真实桌面安装。
type DesktopResolveOptions struct {
	GOOS              string
	Home              string
	Explicit          string
	SearchRoots       []string
	SearchRootScanner func(root string) (string, error)
	IsExecutable      func(path string) bool
}

// ResolveCodexDesktopApp 解析 Linux Codex / ChatGPT 桌面应用可执行文件。
// 显式 AGW_CODEX_APP 优先；无显式值时仅支持 Linux 自动发现。
func ResolveCodexDesktopApp(opts DesktopResolveOptions) (string, error) {
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOOS != "linux" {
		return "", fmt.Errorf("codex-desktop 自动发现仅支持 Linux，当前平台 %s；本变更不支持 macOS/Windows 桌面应用", opts.GOOS)
	}
	if opts.IsExecutable == nil {
		opts.IsExecutable = unixExecutable
	}
	if opts.Explicit != "" {
		return resolveExplicitDesktopApp(opts.Explicit, opts.IsExecutable)
	}

	roots := opts.SearchRoots
	if len(roots) == 0 {
		roots = DefaultLinuxDesktopRoots(opts.Home)
	}
	if opts.SearchRootScanner != nil {
		for _, root := range roots {
			path, err := opts.SearchRootScanner(root)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return "", fmt.Errorf("扫描 Linux 桌面应用目录 %s 失败: %w", root, err)
			}
			if path != "" {
				return path, nil
			}
		}
	} else {
		for _, root := range roots {
			path, err := findLinuxDesktopAppInRoot(root, opts.IsExecutable)
			if err != nil {
				return "", err
			}
			if path != "" {
				return path, nil
			}
		}
	}

	rootsForError := roots
	if len(rootsForError) == 0 {
		rootsForError = DefaultLinuxDesktopRoots(opts.Home)
	}
	return "", missingDesktopAppError(rootsForError)
}

// DefaultLinuxDesktopRoots 返回有限的 Linux 默认搜索目录，顺序固定。
func DefaultLinuxDesktopRoots(home string) []string {
	roots := []string{"/usr/lib", "/opt"}
	if home != "" {
		roots = append(roots,
			filepath.Join(home, "Applications"),
			filepath.Join(home, ".local/share"),
		)
	}
	return roots
}

func resolveExplicitDesktopApp(explicit string, isExecutable func(string) bool) (string, error) {
	info, err := os.Stat(explicit)
	if err != nil {
		return "", fmt.Errorf("AGW_CODEX_APP 不存在或不可访问 %s: %w", explicit, err)
	}
	if info.IsDir() {
		return executableInDesktopAppDir(explicit, isExecutable)
	}
	if !isExecutable(explicit) {
		return "", fmt.Errorf("AGW_CODEX_APP 不是可执行文件: %s", explicit)
	}
	return filepath.Clean(explicit), nil
}

func findLinuxDesktopAppInRoot(root string, isExecutable func(string) bool) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("扫描 Linux 桌面应用目录 %s 失败: %w", root, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if !isLinuxDesktopAppDirName(name) {
			continue
		}
		dir := filepath.Join(root, name)
		path, _ := executableInDesktopAppDir(dir, isExecutable)
		if path != "" {
			return path, nil
		}
		if appDir := filepath.Join(dir, "app"); dirExists(appDir) {
			path, err := executableInDesktopAppDir(appDir, isExecutable)
			if err != nil {
				return "", err
			}
			if path != "" {
				return path, nil
			}
		}
	}
	return "", nil
}

func executableInDesktopAppDir(dir string, isExecutable func(string) bool) (string, error) {
	for _, name := range linuxDesktopExecutables {
		path := filepath.Join(dir, name)
		if isExecutable(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("在应用目录 %s 中未找到受支持的可执行文件（支持 %s）", dir, strings.Join(linuxDesktopExecutables, ", "))
}

func isLinuxDesktopAppDirName(name string) bool {
	for _, want := range linuxDesktopAppDirs {
		if name == want {
			return true
		}
	}
	return false
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func missingDesktopAppError(roots []string) error {
	return fmt.Errorf(
		"未找到 Linux Codex / ChatGPT 桌面应用；已搜索 roots: %s，应用目录: %s，可执行文件: %s；可设置 AGW_CODEX_APP 指向应用目录或可执行文件",
		strings.Join(roots, ", "),
		strings.Join(linuxDesktopAppDirs, ", "),
		strings.Join(linuxDesktopExecutables, ", "),
	)
}

func unixExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
