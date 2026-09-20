package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"agent_gateway/internal/config"

	"github.com/spf13/cobra"
)

// rootFlag 是全局 --root。
var rootFlag string

// addRootFlag 注册 --root。
func addRootFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&rootFlag, "root", "", "网关仓库根目录（默认从 cwd 向上探测）")
}

// resolveRootE 解析网关仓库根；显式 --root / AGW_ROOT 必须包含 config/。
func resolveRootE() (string, error) {
	explicit := rootFlag
	source := "--root"
	if explicit == "" {
		if v := os.Getenv("AGW_ROOT"); v != "" {
			explicit = v
			source = "AGW_ROOT"
		}
	}
	if explicit != "" {
		if _, err := os.Stat(filepath.Join(explicit, "config")); err != nil {
			return "", fmt.Errorf("%s=%s 不是有效网关仓库", source, explicit)
		}
		return explicit, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("无法获取 cwd: %w", err)
	}
	return config.FindRoot(wd)
}

// resolveRoot 得到网关仓库根；解析失败时直接退出。
func resolveRoot() string {
	root, err := resolveRootE()
	if err != nil {
		fatalf("%v", err)
	}
	return root
}

// loadConfig 读取并校验配置（失败即退出）。
func loadConfig(root string) *config.Config {
	cfg, err := config.Load(root)
	if err != nil {
		fatalf("加载配置失败: %v", err)
	}
	return cfg
}

// runDir / pidPath / logPath 是运行时产物路径。
func runDir(root string) string  { return filepath.Join(root, ".run") }
func pidPath(root string) string { return filepath.Join(root, ".run", "agw.pid") }
func logPath(root string) string { return filepath.Join(root, ".run", "agw.log") }
func adminURL(cfg *config.Config, path string) string {
	return "http://" + cfg.Gateway.Listen + path
}

// adminRequest 向管理端点发请求。
func adminRequest(method, url string, cfg *config.Config) (*http.Response, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
	client := &http.Client{Timeout: 5 * time.Second}
	return client.Do(req)
}

// reloadIfRunning 触发热重载；返回是否真正触发了 reload 与错误。
// 网关未运行时返回 (false, nil) 并打印提示；请求失败返回 (false, err)。
func reloadIfRunning(root string, cfg *config.Config) (bool, error) {
	if !pidAlive(readPid(root)) {
		fmt.Println("提示：网关未运行，配置将在下次启动时生效")
		return false, nil
	}
	resp, err := adminRequest("POST", adminURL(cfg, "/__agw/reload"), cfg)
	if err != nil {
		return false, fmt.Errorf("热重载请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		fmt.Println("已通知网关热重载")
		return true, nil
	}
	return false, fmt.Errorf("热重载返回 HTTP %d", resp.StatusCode)
}
