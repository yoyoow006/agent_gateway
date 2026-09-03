// Package cli 提供 agw 的全部子命令。
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewRootCmd 构造 agw 根命令；子命令在各文件中通过 init 注册到 rootCommands。
var rootCommands = []*cobra.Command{}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agw",
		Short: "agw —— 本地大模型 API 路由网关（Claude Code / Codex 适配）",
		Long: `agw 在 agent 与大模型供应商之间做本地路由：
agent 一次性指向网关，网关负责协议转换、故障切换与项目档案路由，
切换供应商不需要重启 agent。`,
		SilenceUsage: true,
	}
	for _, cmd := range rootCommands {
		root.AddCommand(cmd)
	}
	return root
}

// Execute 运行根命令，错误已由 cobra 输出。
func Execute() error {
	return NewRootCmd().Execute()
}

// fatalf 在子命令中打印错误并以非零码退出。
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "agw: "+format+"\n", args...)
	os.Exit(1)
}
