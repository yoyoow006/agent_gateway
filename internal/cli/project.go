package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"agent_gateway/internal/workspace"

	"github.com/spf13/cobra"
)

var (
	projectCmd  = &cobra.Command{Use: "project", Short: "管理 projects/ 业务项目工作区"}
	projectNew  = &cobra.Command{Use: "new <名称>", Short: "创建项目（目录+git init+agw.toml+令牌）", Args: cobra.ExactArgs(1), Run: runProjectNew}
	projectList = &cobra.Command{Use: "list", Short: "列出项目与 git/覆盖摘要", Run: runProjectList}
)

func init() {
	addRootFlag(projectNew)
	addRootFlag(projectList)
	projectCmd.AddCommand(projectNew, projectList)
	rootCommands = append(rootCommands, projectCmd)
}

func runProjectNew(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	if _, err := workspace.New(root, args[0], nil); err != nil {
		fatalf("%v", err)
	}
}

func runProjectList(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	items, err := workspace.List(root, nil)
	if err != nil {
		fatalf("%v", err)
	}
	if len(items) == 0 {
		fmt.Println("（暂无项目；agw project new <名称> 创建）")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "项目\t分支\t脏\t供应商覆盖\t粘性\t模型映射")
	for _, it := range items {
		providers := "-"
		if len(it.Providers) > 0 {
			providers = fmt.Sprintf("%d", len(it.Providers))
		}
		fmt.Fprintf(tw, "%s\t%s\t%v\t%s\t%s\t%d\n", it.Name, it.Branch, it.Dirty, providers, it.Preferred, it.ModelMap)
	}
	tw.Flush()
}
