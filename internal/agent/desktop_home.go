package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent_gateway/internal/config"

	"github.com/BurntSushi/toml"
)

const desktopHomeRelative = ".agw/codex-desktop"

const desktopConfigTemplate = `model_provider = %q
disable_response_storage = true

[model_providers.agw]
name = "agw"
base_url = %q
wire_api = "responses"
env_key = "AGW_API_KEY"
`

type desktopWritePlan struct {
	home       string
	configPath string
	authPath   string
	configData []byte
	authData   []byte
}

// DesktopHome 返回 agw 管理的 Codex 桌面独立 home。
func DesktopHome(root string) string {
	return filepath.Join(root, filepath.FromSlash(desktopHomeRelative))
}

// EnsureDesktopHome 校验并生成 agw 管理的独立 Codex 桌面配置与认证文件。
func EnsureDesktopHome(root, listen, token string) (string, error) {
	return EnsureDesktopHomeWithFailer(root, listen, token, nil)
}

// EnsureDesktopHomeWithFailer 供原子替换失败注入测试使用。
func EnsureDesktopHomeWithFailer(root, listen, token string, failer func(path string) error) (string, error) {
	baseURL, err := config.BaseURL(listen)
	if err != nil {
		return "", err
	}
	home, err := validateDesktopHomePath(root)
	if err != nil {
		return "", err
	}
	plan := desktopWritePlan{
		home:       filepath.Clean(home),
		configPath: filepath.Join(home, "config.toml"),
		authPath:   filepath.Join(home, "auth.json"),
		configData: []byte(fmt.Sprintf(
			desktopConfigTemplate, "agw", baseURL+"/v1",
		)),
		authData: mustDesktopAuth(token),
	}
	if err := os.MkdirAll(plan.home, 0o700); err != nil {
		return "", err
	}
	if err := validateManagedDesktopHome(plan); err != nil {
		return "", err
	}

	configInfo, configStatErr := os.Stat(plan.configPath)
	authInfo, authStatErr := os.Stat(plan.authPath)
	if configStatErr != nil && !os.IsNotExist(configStatErr) {
		return "", configStatErr
	}
	if authStatErr != nil && !os.IsNotExist(authStatErr) {
		return "", authStatErr
	}
	configExists := configStatErr == nil
	authExists := authStatErr == nil
	if configExists && authExists &&
		fileContains(plan.configPath, plan.configData) &&
		fileContains(plan.authPath, plan.authData) &&
		configInfo.Mode().Perm() == 0o600 && authInfo.Mode().Perm() == 0o600 {
		return plan.home, nil
	}

	oldConfig, oldConfigExists, err := readOptionalFile(plan.configPath)
	if err != nil {
		return "", err
	}
	oldAuth, oldAuthExists, err := readOptionalFile(plan.authPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(plan.home, "backups"), 0o700); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().UnixNano()
	if err := writeDesktopBackup(plan.home, stamp, "config.toml", oldConfig, oldConfigExists); err != nil {
		return "", err
	}
	if err := writeDesktopBackup(plan.home, stamp, "auth.json", oldAuth, oldAuthExists); err != nil {
		return "", err
	}

	committed := false
	changed := false
	defer func() {
		if changed && !committed {
			rollbackDesktopPair(plan, oldConfig, oldConfigExists, oldAuth, oldAuthExists)
		}
	}()
	if err := atomicWriteDesktopFile(plan.configPath, plan.configData, failer); err != nil {
		return "", err
	}
	changed = true
	if err := atomicWriteDesktopFile(plan.authPath, plan.authData, failer); err != nil {
		return "", err
	}
	committed = true
	return plan.home, nil
}

// ResetDesktopHome 仅删除并重建 agw 管理的独立桌面 home。
func ResetDesktopHome(root string) error {
	home, err := validateDesktopHomePath(root)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(home); err != nil {
		return fmt.Errorf("重置 agw Codex 桌面 home 失败: %w", err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	return os.Chmod(home, 0o700)
}

func validateDesktopHomePath(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absHome, err := filepath.Abs(DesktopHome(root))
	if err != nil {
		return "", err
	}
	if absRoot == string(filepath.Separator) || absHome == string(filepath.Separator) {
		return "", fmt.Errorf("拒绝使用文件系统根作为 agw 路径")
	}
	if absHome != filepath.Clean(filepath.Join(absRoot, filepath.FromSlash(desktopHomeRelative))) {
		return "", fmt.Errorf("agw Codex 桌面 home 路径异常: %s", absHome)
	}
	if info, err := os.Lstat(filepath.Join(absRoot, ".agw", "codex-desktop")); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("agw Codex 桌面 home 是符号链接，可能逃逸网关根: %s", absHome)
	}
	// 若目标存在，canonical 后仍必须位于 canonical root 内。
	if _, err := os.Stat(absHome); err == nil {
		canonicalRoot, rootErr := filepath.EvalSymlinks(absRoot)
		if rootErr != nil {
			return "", rootErr
		}
		canonicalHome, homeErr := filepath.EvalSymlinks(absHome)
		if homeErr != nil {
			return "", homeErr
		}
		if !strings.HasPrefix(canonicalHome+string(filepath.Separator), canonicalRoot+string(filepath.Separator)) {
			return "", fmt.Errorf("agw Codex 桌面 home 符号链接逃逸网关根: %s -> %s", absHome, canonicalHome)
		}
	}
	return absHome, nil
}

func validateManagedDesktopHome(plan desktopWritePlan) error {
	if _, err := os.Stat(plan.home); os.IsNotExist(err) {
		return nil
	}
	entries, err := os.ReadDir(plan.home)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "config.toml" || name == "auth.json" || name == "backups" {
			continue
		}
		return fmt.Errorf("agw Codex 桌面 home 包含未识别条目 %s；确认无需保留后可执行 agw run codex-desktop --reset", name)
	}
	if _, err := os.Stat(plan.configPath); err == nil {
		data, err := os.ReadFile(plan.configPath)
		if err != nil {
			return err
		}
		var cfg map[string]any
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return fmt.Errorf("现有 agw Codex 桌面 config.toml 无效: %w", err)
		}
	}
	if _, err := os.Stat(plan.authPath); err == nil {
		data, err := os.ReadFile(plan.authPath)
		if err != nil {
			return err
		}
		var auth map[string]any
		if err := json.Unmarshal(data, &auth); err != nil {
			return fmt.Errorf("现有 agw Codex 桌面 auth.json 无效: %w", err)
		}
	}
	return nil
}

func mustDesktopAuth(token string) []byte {
	data, err := json.MarshalIndent(map[string]string{"OPENAI_API_KEY": token}, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

func fileContains(path string, want []byte) bool {
	data, err := os.ReadFile(path)
	return err == nil && string(data) == string(want)
}

func readOptionalFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func writeDesktopBackup(home string, stamp int64, name string, data []byte, exists bool) error {
	if !exists {
		data = []byte{}
	}
	path := filepath.Join(home, "backups", fmt.Sprintf("%019d-%s", stamp, name))
	return atomicWriteDesktopFile(path, data, nil)
}

func atomicWriteDesktopFile(path string, data []byte, failer func(path string) error) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if failer != nil {
		if err := failer(tmp); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return os.Rename(tmp, path)
}

func rollbackDesktopPair(plan desktopWritePlan, oldConfig []byte, oldConfigExists bool, oldAuth []byte, oldAuthExists bool) {
	if err := restoreOptionalFile(plan.configPath, oldConfig, oldConfigExists); err != nil {
		fmt.Fprintf(os.Stderr, "agw: 回滚 config.toml 失败: %v\n", err)
	}
	if err := restoreOptionalFile(plan.authPath, oldAuth, oldAuthExists); err != nil {
		fmt.Fprintf(os.Stderr, "agw: 回滚 auth.json 失败: %v\n", err)
	}
}

func restoreOptionalFile(path string, data []byte, exists bool) error {
	if !exists {
		if err := os.Remove(path); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		return nil
	}
	return atomicWriteDesktopFile(path, data, nil)
}
