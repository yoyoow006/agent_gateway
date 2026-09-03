package cli

import (
	"fmt"
	"os"

	"agent_gateway/internal/agent"
	"agent_gateway/internal/config"

	"github.com/spf13/cobra"
)

var (
	installCmd = &cobra.Command{
		Use:   "install <claude|codex>",
		Short: "一键安装 agent 并生成独立配置（npm + 零接触用户默认文件）",
		Args:  cobra.ExactArgs(1),
		Run:   runInstall,
	}
	runCmd = &cobra.Command{
		Use:   "run <claude|codex> [--project 名] [-- agent 参数...]",
		Short: "在项目上下文中启动 agent（注入项目令牌，exec 替换进程）",
		// `--` 之后的参数会被并入 positional args，不能再用 ExactArgs(1)
		Args: cobra.MinimumNArgs(1),
		Run:  runAgent,
	}
)

func init() {
	addRootFlag(installCmd)
	addRootFlag(runCmd)
	runCmd.Flags().StringP("project", "p", "", "项目名（缺省按 cwd 推断，不在 projects/ 下则用全局档案）")
	rootCommands = append(rootCommands, installCmd, runCmd)
}

func runInstall(cmd *cobra.Command, args []string) {
	kind := args[0]
	if kind != agent.KindClaude && kind != agent.KindCodex {
		fatalf("未知 agent: %s（claude | codex）", kind)
	}
	root := resolveRoot()
	cfg := loadConfig(root)
	if err := config.EnsureSecrets(root, cfg); err != nil {
		fatalf("%v", err)
	}
	opts := agent.Options{Listen: cfg.Gateway.Listen, Root: root}
	var err error
	if kind == agent.KindClaude {
		err = agent.InstallClaude(opts)
	} else {
		err = agent.InstallCodex(opts)
	}
	if err != nil {
		fatalf("%v", err)
	}
}

// execAgent 可注入的 exec 入口（测试替换）。
var execAgent = agent.Exec

func runAgent(cmd *cobra.Command, args []string) {
	kind := args[0]
	if kind != agent.KindClaude && kind != agent.KindCodex {
		fatalf("未知 agent: %s（claude | codex）", kind)
	}
	root := resolveRoot()
	project, _ := cmd.Flags().GetString("project")
	extra := extractAgentArgs(os.Args)
	env, dir, argv, err := agent.PrepareExec(root, kind, project, extra)
	if err != nil {
		fatalf("%v", err)
	}
	fmt.Fprintf(os.Stderr, "agw: 启动 %s（项目=%s 目录=%s）\n", kind, project, dir)
	if err := execAgent(env, dir, argv); err != nil {
		fatalf("%v", err)
	}
}

// extractAgentArgs 提取 `--` 之后的参数透传给 agent。
func extractAgentArgs(osArgs []string) []string {
	for i, a := range osArgs {
		if a == "--" {
			return osArgs[i+1:]
		}
	}
	return nil
}
