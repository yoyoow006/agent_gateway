package cli

import (
	"errors"
	"fmt"
	"os"

	"agent_gateway/internal/config"

	"github.com/spf13/cobra"
)

// ErrReloadFailed 标记 reload 请求失败（区别于本地解析失败），main 据此区分退出码 2 与 1。
var ErrReloadFailed = errors.New("热重载请求失败")

// reloadCmd 不写盘、仅触发运行中网关的热重载；网关未运行时幂等退出 0。
var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "触发网关热重载（不写盘；网关未运行时幂等退出）",
	Long: `向运行中的网关 POST /__agw/reload，让它重新读取 config/local.toml 与项目配置。
不会改动任何文件——若需先编辑配置请用编辑器。
网关未运行时退出 0，提示"将在下次启动时生效"。`,
	RunE: runReload,
}

func init() {
	addRootFlag(reloadCmd)
	rootCommands = append(rootCommands, reloadCmd)
}

func runReload(cmd *cobra.Command, args []string) error {
	root := resolveRoot()
	// local.toml 解析失败属于本地输入错误：返回普通 error，main.go 退出码 1。
	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	// 网关运行中时由 reloadIfRunning 处理请求失败：返回 ErrReloadFailed → 退出码 2。
	if _, err := reloadIfRunning(root, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "警告：%v；网关重启后生效\n", err)
		return fmt.Errorf("%w: %v", ErrReloadFailed, err)
	}
	return nil
}